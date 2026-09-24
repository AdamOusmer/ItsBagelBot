// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsmigrate

import (
	"fmt"
	"io"
	"time"

	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
)

type RestoreOptions struct {
	Domain    string
	ChunkSize int
	Timeout   time.Duration
}

type RestoreStats struct {
	Stream string
	Bytes  int64
}

type restoreRequest struct {
	Config nats.StreamConfig `json:"config"`
	State  nats.StreamState  `json:"state"`
}

type restoreResponse struct {
	Type           string         `json:"type"`
	Error          *nats.APIError `json:"error,omitempty"`
	DeliverSubject string         `json:"deliver_subject,omitempty"`
}

type restoreCompleteResponse struct {
	Type  string         `json:"type"`
	Error *nats.APIError `json:"error,omitempty"`
	nats.StreamInfo
}

// restoreChannel is the subject pair a restore streams chunks over: deliver
// carries chunk data to the server, ackInbox carries the per-chunk acks back.
type restoreChannel struct {
	deliver  string
	ackInbox string
	ackSub   *nats.Subscription
	timeout  time.Duration
}

// Restore reads a snapshot written by Snapshot from r and replays it onto nc
// using the raw $JS.API.STREAM.RESTORE protocol, including its consumers.
func Restore(nc *nats.Conn, r io.Reader, opts RestoreOptions) (RestoreStats, error) {
	header, body, err := readSnapshotHeader(r)
	if err != nil {
		return RestoreStats{}, err
	}
	resp, err := requestRestore(nc, header, opts)
	if err != nil {
		return RestoreStats{}, err
	}
	bytes, err := sendRestoreChunks(nc, resp.DeliverSubject, body, opts)
	if err != nil {
		return RestoreStats{Stream: header.Stream, Bytes: bytes}, fmt.Errorf("natsmigrate: restore %q: %w", header.Stream, err)
	}
	return RestoreStats{Stream: header.Stream, Bytes: bytes}, nil
}

func readSnapshotHeader(r io.Reader) (snapshotHeader, io.Reader, error) {
	dec := codec.NewDecoder(r)
	var header snapshotHeader
	if err := dec.Decode(&header); err != nil {
		return snapshotHeader{}, nil, fmt.Errorf("natsmigrate: decode snapshot header: %w", err)
	}
	return header, io.MultiReader(dec.Buffered(), r), nil
}

func requestRestore(nc *nats.Conn, header snapshotHeader, opts RestoreOptions) (*restoreResponse, error) {
	payload, err := codec.Marshal(restoreRequest{Config: header.Config, State: header.State})
	if err != nil {
		return nil, fmt.Errorf("natsmigrate: encode restore request for %q: %w", header.Stream, err)
	}
	timeout := withDefaultDuration(opts.Timeout, DefaultTimeout)
	subject := apiSubject(opts.Domain, "STREAM.RESTORE."+header.Stream)
	msg, err := nc.Request(subject, payload, timeout)
	if err != nil {
		return nil, fmt.Errorf("natsmigrate: request restore for %q: %w", header.Stream, err)
	}
	var resp restoreResponse
	if err := codec.Unmarshal(msg.Data, &resp); err != nil {
		return nil, fmt.Errorf("natsmigrate: decode restore response for %q: %w", header.Stream, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("natsmigrate: restore %q refused: %s", header.Stream, resp.Error.Description)
	}
	return &resp, nil
}

func sendRestoreChunks(nc *nats.Conn, deliver string, body io.Reader, opts RestoreOptions) (int64, error) {
	ch, err := openRestoreChannel(nc, deliver, opts)
	if err != nil {
		return 0, err
	}
	defer func() { _ = ch.ackSub.Unsubscribe() }()

	chunkSize := withDefaultInt(opts.ChunkSize, DefaultChunkSize)
	total, err := streamRestoreBody(nc, ch, body, chunkSize)
	if err != nil {
		return total, err
	}
	return total, finishRestore(nc, ch)
}

func openRestoreChannel(nc *nats.Conn, deliver string, opts RestoreOptions) (restoreChannel, error) {
	ackInbox := nc.NewInbox()
	ackSub, err := nc.SubscribeSync(ackInbox)
	if err != nil {
		return restoreChannel{}, fmt.Errorf("subscribe restore ack inbox: %w", err)
	}
	timeout := withDefaultDuration(opts.Timeout, DefaultTimeout)
	return restoreChannel{deliver: deliver, ackInbox: ackInbox, ackSub: ackSub, timeout: timeout}, nil
}

func streamRestoreBody(nc *nats.Conn, ch restoreChannel, body io.Reader, chunkSize int) (int64, error) {
	buf := make([]byte, chunkSize)
	var total int64
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if pubErr := publishRestoreChunk(nc, ch, buf[:n]); pubErr != nil {
				return total, pubErr
			}
			total += int64(n)
		}
		if err == io.EOF {
			return total, nil
		}
		if err != nil {
			return total, fmt.Errorf("read snapshot body: %w", err)
		}
	}
}

func publishRestoreChunk(nc *nats.Conn, ch restoreChannel, chunk []byte) error {
	if err := nc.PublishRequest(ch.deliver, ch.ackInbox, chunk); err != nil {
		return fmt.Errorf("publish restore chunk: %w", err)
	}
	if _, err := ch.ackSub.NextMsg(ch.timeout); err != nil {
		return fmt.Errorf("wait for restore chunk ack: %w", err)
	}
	return nil
}

func finishRestore(nc *nats.Conn, ch restoreChannel) error {
	if err := nc.PublishRequest(ch.deliver, ch.ackInbox, nil); err != nil {
		return fmt.Errorf("publish restore completion: %w", err)
	}
	msg, err := ch.ackSub.NextMsg(ch.timeout)
	if err != nil {
		return fmt.Errorf("wait for restore completion: %w", err)
	}
	var resp restoreCompleteResponse
	if err := codec.Unmarshal(msg.Data, &resp); err != nil {
		return fmt.Errorf("decode restore completion: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("restore refused: %s", resp.Error.Description)
	}
	return nil
}
