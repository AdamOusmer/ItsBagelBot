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

type SnapshotOptions struct {
	Domain      string
	NoConsumers bool
	ChunkSize   int
	WindowSize  int
	Timeout     time.Duration
}

type SnapshotStats struct {
	Stream string
	Bytes  int64
}

// snapshotHeader is the self-describing prefix written to the snapshot
// writer before the raw $JS.API.STREAM.SNAPSHOT byte stream, so Restore
// only needs an io.Reader.
type snapshotHeader struct {
	Stream string            `json:"stream"`
	Config nats.StreamConfig `json:"config"`
	State  nats.StreamState  `json:"state"`
}

type snapshotRequest struct {
	DeliverSubject string `json:"deliver_subject"`
	NoConsumers    bool   `json:"no_consumers,omitempty"`
	ChunkSize      int    `json:"chunk_size,omitempty"`
	WindowSize     int    `json:"window_size,omitempty"`
}

type snapshotResponse struct {
	Type   string             `json:"type"`
	Error  *nats.APIError     `json:"error,omitempty"`
	Config *nats.StreamConfig `json:"config,omitempty"`
	State  *nats.StreamState  `json:"state,omitempty"`
}

// Snapshot streams stream (including its consumers) from nc to w using the
// raw $JS.API.STREAM.SNAPSHOT protocol.
func Snapshot(nc *nats.Conn, stream string, w io.Writer, opts SnapshotOptions) (SnapshotStats, error) {
	sub, deliver, err := subscribeSnapshotChunks(nc)
	if err != nil {
		return SnapshotStats{}, err
	}
	defer func() { _ = sub.Unsubscribe() }()

	resp, err := requestSnapshot(nc, stream, deliver, opts)
	if err != nil {
		return SnapshotStats{}, err
	}

	header := snapshotHeader{Stream: stream, Config: *resp.Config, State: *resp.State}
	if err := codec.NewEncoder(w).Encode(header); err != nil {
		return SnapshotStats{}, fmt.Errorf("natsmigrate: encode snapshot header for %q: %w", stream, err)
	}

	timeout := withDefaultDuration(opts.Timeout, DefaultTimeout)
	bytes, err := drainSnapshotChunks(nc, sub, w, timeout)
	if err != nil {
		return SnapshotStats{Stream: stream, Bytes: bytes}, fmt.Errorf("natsmigrate: snapshot %q: %w", stream, err)
	}
	return SnapshotStats{Stream: stream, Bytes: bytes}, nil
}

func subscribeSnapshotChunks(nc *nats.Conn) (*nats.Subscription, string, error) {
	deliver := nc.NewInbox()
	sub, err := nc.SubscribeSync(deliver)
	if err != nil {
		return nil, "", fmt.Errorf("natsmigrate: subscribe snapshot deliver subject: %w", err)
	}
	return sub, deliver, nil
}

func requestSnapshot(nc *nats.Conn, stream, deliver string, opts SnapshotOptions) (*snapshotResponse, error) {
	req := snapshotRequest{DeliverSubject: deliver, NoConsumers: opts.NoConsumers, ChunkSize: opts.ChunkSize, WindowSize: opts.WindowSize}
	payload, err := codec.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("natsmigrate: encode snapshot request for %q: %w", stream, err)
	}
	timeout := withDefaultDuration(opts.Timeout, DefaultTimeout)
	subject := apiSubject(opts.Domain, "STREAM.SNAPSHOT."+stream)
	msg, err := nc.Request(subject, payload, timeout)
	if err != nil {
		return nil, fmt.Errorf("natsmigrate: request snapshot for %q: %w", stream, err)
	}
	var resp snapshotResponse
	if err := codec.Unmarshal(msg.Data, &resp); err != nil {
		return nil, fmt.Errorf("natsmigrate: decode snapshot response for %q: %w", stream, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("natsmigrate: snapshot %q refused: %s", stream, resp.Error.Description)
	}
	return &resp, nil
}

func drainSnapshotChunks(nc *nats.Conn, sub *nats.Subscription, w io.Writer, timeout time.Duration) (int64, error) {
	var total int64
	for {
		msg, err := sub.NextMsg(timeout)
		if err != nil {
			return total, fmt.Errorf("wait for snapshot chunk: %w", err)
		}
		if len(msg.Data) == 0 {
			return total, snapshotEndStatus(msg)
		}
		n, err := w.Write(msg.Data)
		total += int64(n)
		if err != nil {
			return total, fmt.Errorf("write snapshot chunk: %w", err)
		}
		if err := ackSnapshotChunk(nc, msg); err != nil {
			return total, err
		}
	}
}

func ackSnapshotChunk(nc *nats.Conn, msg *nats.Msg) error {
	if msg.Reply == "" {
		return nil
	}
	if err := nc.Publish(msg.Reply, nil); err != nil {
		return fmt.Errorf("ack snapshot chunk: %w", err)
	}
	return nil
}

func snapshotEndStatus(msg *nats.Msg) error {
	status := msg.Header.Get("Status")
	if status == "" || status == "204" {
		return nil
	}
	return fmt.Errorf("snapshot ended with status %s: %s", status, msg.Header.Get("Description"))
}
