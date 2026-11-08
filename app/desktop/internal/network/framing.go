// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package network supervises a sandboxed parser and grants fixed raw TCP
// capabilities. No credential store, private key or Wails service enters it.
package network

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"

	"ItsBagelBot/app/desktop/internal/network/wire"
	"ItsBagelBot/pkg/protobuf"
)

const maxFrame = 128 << 10
const maxChunk = 32 << 10
const laneCount = 4
const streamCount = 2
const requestCount = 16
const (
	hello uint32 = iota + 1
	bootstrap
	oauthRequest
	dialRequest
	postRequest
	closeStream
	ackStream
	streamFrame
	streamError
	openLane
	closeLane
	response
	cancelRequest
)
const (
	noError uint32 = iota
	unavailable
	authentication
	protocolError
	nonceError
	closedError
)

var ErrWorker = errors.New("network: isolated worker unavailable")
var ErrProtocol = errors.New("network: isolated worker protocol rejected")

type frames struct {
	reader io.Reader
	buffer [maxFrame]byte
	budget frameBudget
}

func (f *frames) read() (*wire.Frame, error) {
	var header [4]byte
	if _, err := io.ReadFull(f.reader, header[:1]); err != nil {
		return nil, err
	}
	// Idle pipes may wait indefinitely. Once a packet starts it must complete
	// promptly; a compromised worker cannot hold the trusted reader on a prefix.
	if err := f.deadline(time.Now().Add(time.Second)); err != nil {
		return nil, err
	}
	defer f.deadline(time.Time{})
	if _, err := io.ReadFull(f.reader, header[1:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > maxFrame {
		return nil, ErrProtocol
	}
	if !f.budget.allow(int(size), time.Now()) {
		return nil, ErrProtocol
	}
	if _, err := io.ReadFull(f.reader, f.buffer[:size]); err != nil {
		return nil, err
	}
	m := wire.FrameFromVTPool()
	if err := protobuf.Unmarshal(f.buffer[:size], m); err != nil {
		m.ReturnToVTPool()
		return nil, ErrProtocol
	}
	return m, nil
}
func (f *frames) deadline(t time.Time) error {
	if pipe, ok := f.reader.(interface{ SetReadDeadline(time.Time) error }); ok {
		return pipe.SetReadDeadline(t)
	}
	return nil
}

// Size bounds alone do not contain a compromised worker's CPU load. Pipe input
// also has a fixed work budget; excess closes the worker, never grows a queue.
type frameBudget struct {
	at           time.Time
	count, bytes int
}

func (b *frameBudget) allow(size int, now time.Time) bool {
	if b.at.IsZero() || now.Sub(b.at) >= time.Second {
		b.at = now
		b.count = 0
		b.bytes = 0
	}
	b.count++
	b.bytes += size
	return b.count <= 4096 && b.bytes <= 16<<20
}

// Each writer owns a separate buffer from its reader; a single mutex preserves
// whole messages without publishing pooled storage or retaining caller fields.
type sender struct {
	writer io.Writer
	ctx    context.Context
	once   sync.Once
	gate   chan struct{}
	buffer [maxFrame]byte
	header [4]byte
}

func (s *sender) write(m *wire.Frame) error {
	ctx := s.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return s.writeContext(ctx, m)
}
func (s *sender) writeContext(ctx context.Context, m *wire.Frame) error {
	s.once.Do(func() { s.gate = make(chan struct{}, 1) })
	select {
	case s.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-s.gate }()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.deadline(time.Now().Add(time.Second)) != nil {
		return ErrWorker
	}
	defer s.deadline(time.Time{})
	size, err := protobuf.Size(m)
	if err != nil || size == 0 || size > maxFrame {
		return ErrProtocol
	}
	n, err := protobuf.MarshalTo(s.buffer[:], m)
	if err != nil {
		return ErrProtocol
	}
	binary.BigEndian.PutUint32(s.header[:], uint32(n))
	if err = writeAll(s.writer, s.header[:]); err != nil {
		return err
	}
	return writeAll(s.writer, s.buffer[:n])
}
func (s *sender) deadline(t time.Time) error {
	if pipe, ok := s.writer.(interface{ SetWriteDeadline(time.Time) error }); ok {
		return pipe.SetWriteDeadline(t)
	}
	return nil
}
func writeAll(w io.Writer, raw []byte) error {
	for len(raw) > 0 {
		n, err := w.Write(raw)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrNoProgress
		}
		raw = raw[n:]
	}
	return nil
}

// Raw lanes contain encrypted TCP bytes only. The epoch fences late data after
// a connection is replaced; a zero length is EOF for that epoch, not pipe EOF.
type rawWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *rawWriter) write(epoch uint64, body []byte) error {
	if len(body) > maxChunk {
		return ErrProtocol
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	var header [12]byte
	binary.BigEndian.PutUint64(header[:8], epoch)
	binary.BigEndian.PutUint32(header[8:], uint32(len(body)))
	if err := writeAll(w.writer, header[:]); err != nil {
		return err
	}
	return writeAll(w.writer, body)
}
func rawHeader(r io.Reader) (uint64, uint32, error) {
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, 0, err
	}
	size := binary.BigEndian.Uint32(header[8:])
	if size > maxChunk {
		return 0, 0, ErrProtocol
	}
	return binary.BigEndian.Uint64(header[:8]), size, nil
}
