// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/pkg/codec"
)

type scriptedConn struct {
	mu    sync.Mutex
	reads [][]byte
	wrote [][]byte
}

func (s *scriptedConn) Read(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	if len(s.reads) == 0 {
		s.mu.Unlock()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	raw := s.reads[0]
	s.reads = s.reads[1:]
	s.mu.Unlock()
	return raw, nil
}

func (s *scriptedConn) Write(_ context.Context, data []byte) error {
	s.mu.Lock()
	s.wrote = append(s.wrote, data)
	s.mu.Unlock()
	return nil
}

func (s *scriptedConn) Close() error { return nil }

type recHandler struct {
	ready bool
	types []string
}

func (r *recHandler) Ready(context.Context, string, string) error { r.ready = true; return nil }
func (r *recHandler) Dispatch(_ context.Context, t string, _ []byte) error {
	r.types = append(r.types, t)
	return nil
}

func TestSessionIdentifiesAndDispatches(t *testing.T) {
	hello, _ := codec.Marshal(packet{Op: opHello, D: mustRaw(t, helloData{HeartbeatInterval: 50000})})
	ready, _ := codec.Marshal(packet{Op: opDispatch, T: eventReady, D: mustRaw(t, readyData{})})
	join, _ := codec.Marshal(packet{Op: opDispatch, T: eventMemberAdd, D: mustRaw(t, map[string]string{"guild_id": "g"})})
	conn := &scriptedConn{reads: [][]byte{hello, ready, join}}
	h := &recHandler{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	sess := Session{
		Token:  "bot-token",
		Dial:   func(context.Context, string) (Conn, error) { return conn, nil },
		Handle: h,
	}
	_ = sess.oneSocket(ctx, "ws://example")
	if !h.ready {
		t.Fatal("ready not delivered")
	}
	if len(h.types) != 1 || h.types[0] != eventMemberAdd {
		t.Fatalf("dispatch = %v", h.types)
	}
	if len(conn.wrote) == 0 {
		t.Fatal("identify not written")
	}
}

func mustRaw(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := codec.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
