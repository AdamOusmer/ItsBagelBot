// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"ItsBagelBot/app/desktop/internal/network/wire"
	"ItsBagelBot/pkg/sandbox"
)

func TestPartialControlPacketCannotHoldTrustedReader(t *testing.T) {
	reader, writer, err := sandbox.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	input := frames{reader: reader}
	result := make(chan error, 1)
	go func() { _, err := input.read(); result <- err }()
	select {
	case <-result:
		t.Fatal("idle worker pipe failed before packet start")
	case <-time.After(20 * time.Millisecond):
	}
	if _, err = writer.Write([]byte{0}); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-result:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatal("partial packet not rejected by deadline", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("partial packet held reader indefinitely")
	}
}

type readCounter struct {
	reader       io.Reader
	calls, bytes int
}

func (r *readCounter) Read(dst []byte) (int, error) {
	r.calls++
	n, err := r.reader.Read(dst)
	r.bytes += n
	return n, err
}

func frameLength(size uint32) []byte {
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], size)
	return header[:]
}

func TestFrameLengthRejectedBeforePayloadRead(t *testing.T) {
	for _, size := range []uint32{0, maxFrame + 1, ^uint32(0)} {
		reader := &readCounter{reader: bytes.NewReader(frameLength(size))}
		decoder := frames{reader: reader}
		if message, err := decoder.read(); message != nil || !errors.Is(err, ErrProtocol) {
			t.Fatal("invalid private frame length accepted")
		}
		if reader.bytes != 4 {
			t.Fatal("invalid length consumed payload bytes")
		}
	}
}

func TestFrameTruncationAndMalformedProtobufFailClosed(t *testing.T) {
	for _, raw := range [][]byte{{1, 2}, append(frameLength(10), 1, 2, 3), append(frameLength(1), 0x80)} {
		decoder := frames{reader: bytes.NewReader(raw)}
		if frame, err := decoder.read(); frame != nil || err == nil {
			t.Fatal("truncated or malformed private frame accepted")
		}
	}
}

func TestFrameWorkBudgetPrecedesPayloadAndDecode(t *testing.T) {
	for _, budget := range []frameBudget{{at: time.Now(), count: 4096}, {at: time.Now(), bytes: 16 << 20}} {
		reader := &readCounter{reader: bytes.NewReader(append(frameLength(1), 0x80))}
		decoder := frames{reader: reader, budget: budget}
		if _, err := decoder.read(); !errors.Is(err, ErrProtocol) {
			t.Fatal("exhausted frame budget accepted")
		}
		if reader.bytes != 4 {
			t.Fatal("exhausted work budget read or decoded peer payload")
		}
	}
}

func TestFrameCountAndByteBudgetsResetWithoutGrowingQueues(t *testing.T) {
	now := time.Now()
	countBudget := frameBudget{}
	for i := 0; i < 4096; i++ {
		if !countBudget.allow(1, now) {
			t.Fatal("count budget rejected bounded traffic")
		}
	}
	if countBudget.allow(1, now) {
		t.Fatal("frame flood exceeded fixed count budget")
	}
	if !countBudget.allow(1, now.Add(time.Second)) {
		t.Fatal("work budget did not recover in next interval")
	}
	byteBudget := frameBudget{}
	for i := 0; i < 128; i++ {
		if !byteBudget.allow(maxFrame, now) {
			t.Fatal("byte budget rejected bounded traffic")
		}
	}
	if byteBudget.allow(1, now) {
		t.Fatal("peer exceeded fixed byte budget")
	}
}

func TestFrameSenderRejectsUnboundedMessageWithoutWriting(t *testing.T) {
	for _, frame := range []*wire.Frame{{}, {Kind: streamFrame, Body: make([]byte, maxFrame)}} {
		var output bytes.Buffer
		encoder := sender{writer: &output}
		if err := encoder.write(frame); !errors.Is(err, ErrProtocol) {
			t.Fatal("invalid outbound private frame accepted")
		}
		if output.Len() != 0 {
			t.Fatal("outbound size refusal partially published a frame")
		}
	}
}

func TestRawLaneHeaderRejectsOversizeBeforeBody(t *testing.T) {
	var header [12]byte
	binary.BigEndian.PutUint64(header[:8], 7)
	binary.BigEndian.PutUint32(header[8:], maxChunk+1)
	reader := &readCounter{reader: bytes.NewReader(header[:])}
	if _, _, err := rawHeader(reader); !errors.Is(err, ErrProtocol) {
		t.Fatal("oversized encrypted raw lane chunk accepted")
	}
	if reader.calls != 1 || reader.bytes != 12 {
		t.Fatal("raw lane limit consumed payload")
	}
}

func TestFrameDecoderOwnsPayloadBeyondInputBufferReuse(t *testing.T) {
	var output bytes.Buffer
	encoder := sender{writer: &output}
	if err := encoder.write(&wire.Frame{Kind: response, Id: 1, Token: "owned-token", Oauth: &wire.OAuthReply{Refresh: "owned-refresh"}}); err != nil {
		t.Fatal(err)
	}
	if err := encoder.write(&wire.Frame{Kind: response, Id: 2, Token: "new-message"}); err != nil {
		t.Fatal(err)
	}
	decoder := frames{reader: &output}
	first, err := decoder.read()
	if err != nil {
		t.Fatal(err)
	}
	defer first.ReturnToVTPool()
	second, err := decoder.read()
	if err != nil {
		t.Fatal(err)
	}
	defer second.ReturnToVTPool()
	if first.Token != "owned-token" || first.Oauth == nil || first.Oauth.Refresh != "owned-refresh" {
		t.Fatal("reused private pipe buffer corrupted an outstanding response lease")
	}
}
