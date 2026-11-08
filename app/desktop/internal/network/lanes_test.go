// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package network

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

func TestVirtualConnectionRepeatedReuse(t *testing.T) {
	h := newLaneHarness(t, func(context.Context, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() { defer b.Close(); _, _ = io.Copy(b, b) }()
		return a, nil
	})
	var last uint64
	for i := 0; i < 30; i++ {
		c := openHarness(t, h)
		if c.slot != 0 || c.epoch <= last {
			t.Fatalf("lane not reused safely: %d/%d", c.slot, c.epoch)
		}
		last = c.epoch
		_ = c.SetDeadline(time.Now().Add(time.Second))
		if _, err := c.Write([]byte("encrypted bytes")); err != nil {
			t.Fatal(err)
		}
		body := make([]byte, len("encrypted bytes"))
		if _, err := io.ReadFull(c, body); err != nil || string(body) != "encrypted bytes" {
			t.Fatalf("%q %v", body, err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		if err := c.SetReadDeadline(time.Now()); !errors.Is(err, net.ErrClosed) {
			t.Fatalf("old connection changed deadline: %v", err)
		}
	}
}
func TestVirtualCloseUnblocksReadBeforeReuse(t *testing.T) {
	h := newLaneHarness(t, func(context.Context, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() { defer b.Close(); _, _ = io.Copy(io.Discard, b) }()
		return a, nil
	})
	c := openHarness(t, h)
	read := make(chan error, 1)
	go func() { _, err := c.Read(make([]byte, 1)); read <- err }()
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-read:
		if err == nil {
			t.Fatal("read survived Close")
		}
	case <-time.After(time.Second):
		t.Fatal("read blocked")
	}
	next := openHarness(t, h)
	defer next.Close()
	if next.slot != c.slot || next.epoch == c.epoch {
		t.Fatal("lane was not recycled")
	}
	if _, err := c.Write([]byte("old")); !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
}
func standaloneConnection(t *testing.T) (*virtualConn, *os.File, *os.File) {
	t.Helper()
	incoming, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	reader, outgoing, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	lane := &virtualLane{input: incoming, output: outgoing, writer: rawWriter{writer: outgoing}, busy: true, epoch: 1}
	owner := &virtualDialer{}
	owner.lanes[0] = lane
	t.Cleanup(func() { closeFiles([]*os.File{incoming, writer, reader, outgoing}) })
	return &virtualConn{owner: owner, lane: lane, epoch: 1}, writer, reader
}
func encodedRaw(epoch uint64, body []byte) []byte {
	raw := make([]byte, 12+len(body))
	binary.BigEndian.PutUint64(raw[:8], epoch)
	binary.BigEndian.PutUint32(raw[8:12], uint32(len(body)))
	copy(raw[12:], body)
	return raw
}
func TestVirtualReadResumesPartialHeaderAndBody(t *testing.T) {
	for _, cut := range []int{5, 14} {
		t.Run(string(rune('a'+cut)), func(t *testing.T) {
			c, writer, _ := standaloneConnection(t)
			raw := encodedRaw(1, []byte("hello"))
			if _, err := writer.Write(raw[:cut]); err != nil {
				t.Fatal(err)
			}
			_ = c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
			var body [5]byte
			if _, err := c.Read(body[:]); !errors.Is(err, os.ErrDeadlineExceeded) {
				t.Fatalf("expected partial timeout: %v", err)
			}
			_ = c.SetReadDeadline(time.Time{})
			go func() { _, _ = writer.Write(raw[cut:]) }()
			if _, err := io.ReadFull(c, body[:]); err != nil || string(body[:]) != "hello" {
				t.Fatalf("%q %v", body, err)
			}
			if c.lane.quarantined {
				t.Fatal("recoverable timeout poisoned lane")
			}
		})
	}
}
func TestVirtualMalformedFrameQuarantines(t *testing.T) {
	c, writer, _ := standaloneConnection(t)
	go func() { _, _ = writer.Write(encodedRaw(2, []byte("future"))) }()
	if _, err := c.Read(make([]byte, 1)); !errors.Is(err, ErrProtocol) {
		t.Fatal(err)
	}
	if !c.lane.quarantined {
		t.Fatal("corrupt lane remained usable")
	}
}
func TestVirtualPartialWriteQuarantines(t *testing.T) {
	c, _, reader := standaloneConnection(t)
	_ = reader.Close()
	if _, err := c.Write([]byte("encrypted")); err == nil {
		t.Fatal("expected failed write")
	}
	if !c.lane.quarantined {
		t.Fatal("partially written framing remained reusable")
	}
}
func TestVirtualRejectedOpenCanReuseImmediately(t *testing.T) {
	h := newLaneHarness(t, func(context.Context, string) (net.Conn, error) { return nil, io.EOF })
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := h.dialer.DialContext(ctx, "tcp", "auth.example:443")
		cancel()
		if !errors.Is(err, ErrWorker) {
			t.Fatal(err)
		}
	}
	h.dialer.mu.Lock()
	defer h.dialer.mu.Unlock()
	if h.dialer.lanes[0].epoch != 10 || h.dialer.lanes[0].quarantined {
		t.Fatal("failed open drained nonexistent EOF or poisoned lane")
	}
}
func TestVirtualOriginPinned(t *testing.T) {
	h := newLaneHarness(t, func(context.Context, string) (net.Conn, error) { t.Error("unexpected dial"); return nil, io.EOF })
	for _, address := range []string{"localhost:443", "127.0.0.1:443", "auth.example:80", "AUTH.example:443", "auth.example.evil:443"} {
		if _, err := h.dialer.DialContext(context.Background(), "tcp", address); !errors.Is(err, ErrProtocol) {
			t.Fatalf("%s: %v", address, err)
		}
	}
}
func TestWorkerDescriptorsRejectAliases(t *testing.T) {
	for _, args := range [][]string{nil, {"--sandbox-fds=3,4,5,6,7,8,9,3"}, {"--sandbox-fds=1,4,5,6,7,8,9,10"}, {"--sandbox-fds=3,4,5,6,7,8,9,10", "--sandbox-fds=3,4,5,6,7,8,9,10"}, {"--sandbox-fds=3,4,5,6,7,8,9"}} {
		if files, err := parseWorkerFiles(args); err == nil {
			closeFiles(files)
			t.Fatal("accepted invalid descriptor capability list")
		}
	}
}
