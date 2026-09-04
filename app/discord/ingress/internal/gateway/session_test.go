// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"fmt"
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
	// Copy rather than retain data as-is: codec.Marshal's returned slice can
	// alias a pooled encoder buffer that a later, concurrent Marshal call
	// (heartbeat and presenceLoop each run on their own goroutine) is free
	// to reuse. Retaining the original slice made presence's ticker tests
	// race with themselves under -race even though nothing about the
	// gateway code itself was unsynchronized -- the race was this fake
	// holding a live view into memory codec.Marshal no longer owned.
	cp := append([]byte(nil), data...)
	s.mu.Lock()
	s.wrote = append(s.wrote, cp)
	s.mu.Unlock()
	return nil
}

func (s *scriptedConn) Close() error { return nil }

// wroteSnapshot returns a lock-protected copy of what has been written so
// far. Presence tests read this after a background heartbeat/presenceLoop
// goroutine may still be mid-write (oneSocket returns on ctx cancellation,
// which those goroutines only notice on their next select), so reading the
// field directly would race with scriptedConn.Write's own locked append.
func (s *scriptedConn) wroteSnapshot() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.wrote...)
}

type recHandler struct {
	ready bool
	types []string
}

func (r *recHandler) Ready(context.Context, Identity) error { r.ready = true; return nil }
func (r *recHandler) Dispatch(_ context.Context, ev Event) error {
	r.types = append(r.types, ev.Type)
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
	if len(conn.wroteSnapshot()) == 0 {
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

// fakePresence is a scripted gateway.PresenceSource: ok controls whether
// Refresh reports a send, and every call is counted so tests can assert
// Forget ran (the reconnect-resend hook) without depending on wall-clock
// ticker timing.
type fakePresence struct {
	mu        sync.Mutex
	ok        bool
	refreshes int
	forgets   int
}

func (f *fakePresence) Refresh(context.Context) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshes++
	if !f.ok {
		return "", false
	}
	return fmt.Sprintf("watch-%d streams", f.refreshes), true
}

func (f *fakePresence) Forget() {
	f.mu.Lock()
	f.forgets++
	f.mu.Unlock()
}

func (f *fakePresence) snapshot() (refreshes, forgets int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshes, f.forgets
}

// presenceOps decodes every frame conn wrote and returns the "d.activities"
// name of each Update Presence (op 3) frame, in write order.
func presenceOps(t *testing.T, wrote [][]byte) []string {
	t.Helper()
	var names []string
	for _, raw := range wrote {
		var pkt struct {
			Op int `json:"op"`
			D  struct {
				Activities []struct {
					Name string `json:"name"`
					Type int    `json:"type"`
				} `json:"activities"`
			} `json:"d"`
		}
		if err := codec.Unmarshal(raw, &pkt); err != nil {
			t.Fatal(err)
		}
		if pkt.Op != opPresenceUpdate {
			continue
		}
		if len(pkt.D.Activities) != 1 {
			t.Fatalf("presence frame has %d activities, want 1", len(pkt.D.Activities))
		}
		if pkt.D.Activities[0].Type != activityTypeWatching {
			t.Fatalf("activity type = %d, want %d (Watching)", pkt.D.Activities[0].Type, activityTypeWatching)
		}
		names = append(names, pkt.D.Activities[0].Name)
	}
	return names
}

// TestPresenceSentOnConnect is the reconnect-survival hook itself: a fresh
// Identify (onHello) must resend presence immediately, via Forget +
// Refresh, rather than waiting for the next ticker tick which may be minutes
// away. A large PresenceInterval proves the send seen here came from the
// connect path, not the ticker.
func TestPresenceSentOnConnect(t *testing.T) {
	hello, _ := codec.Marshal(packet{Op: opHello, D: mustRaw(t, helloData{HeartbeatInterval: 50000})})
	conn := &scriptedConn{reads: [][]byte{hello}}
	pres := &fakePresence{ok: true}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	sess := Session{
		Token:            "bot-token",
		Dial:             func(context.Context, string) (Conn, error) { return conn, nil },
		Handle:           &recHandler{},
		Presence:         pres,
		PresenceInterval: time.Hour,
	}
	_ = sess.oneSocket(ctx, "ws://example")

	names := presenceOps(t, conn.wroteSnapshot())
	if len(names) != 1 || names[0] != "watch-1 streams" {
		t.Fatalf("presence frames = %v, want exactly one connect-time send", names)
	}
	if _, forgets := pres.snapshot(); forgets != 1 {
		t.Fatalf("forgets = %d, want 1 (reconnect must clear dedup)", forgets)
	}
}

// TestPresenceRefreshesOnTicker proves the second hook: once connected, a
// live socket keeps refreshing presence on PresenceInterval, not just once
// at connect.
func TestPresenceRefreshesOnTicker(t *testing.T) {
	hello, _ := codec.Marshal(packet{Op: opHello, D: mustRaw(t, helloData{HeartbeatInterval: 50000})})
	conn := &scriptedConn{reads: [][]byte{hello}}
	pres := &fakePresence{ok: true}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()
	sess := Session{
		Token:            "bot-token",
		Dial:             func(context.Context, string) (Conn, error) { return conn, nil },
		Handle:           &recHandler{},
		Presence:         pres,
		PresenceInterval: 20 * time.Millisecond,
	}
	_ = sess.oneSocket(ctx, "ws://example")

	names := presenceOps(t, conn.wroteSnapshot())
	if len(names) < 2 {
		t.Fatalf("presence frames = %v, want at least the connect send plus a ticker refresh", names)
	}
}

// TestPresenceSkippedWhenSourceReportsNoChange covers both the dedup path
// (ticker) and the RPC-failure path (Source.Refresh, unit-tested in
// package presence) from the gateway's side: either way, PresenceSource
// reports ok=false, and Session must neither write an Update Presence frame
// nor let that stop the socket -- Identify still goes out and oneSocket
// returns cleanly on context cancellation, same as with no Presence source
// wired at all.
func TestPresenceSkippedWhenSourceReportsNoChange(t *testing.T) {
	hello, _ := codec.Marshal(packet{Op: opHello, D: mustRaw(t, helloData{HeartbeatInterval: 50000})})
	conn := &scriptedConn{reads: [][]byte{hello}}
	pres := &fakePresence{ok: false}
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	sess := Session{
		Token:            "bot-token",
		Dial:             func(context.Context, string) (Conn, error) { return conn, nil },
		Handle:           &recHandler{},
		Presence:         pres,
		PresenceInterval: 15 * time.Millisecond,
	}
	err := sess.oneSocket(ctx, "ws://example")
	if err == nil || ctx.Err() == nil {
		t.Fatalf("oneSocket should end on context cancellation, err=%v ctxErr=%v", err, ctx.Err())
	}

	if names := presenceOps(t, conn.wroteSnapshot()); len(names) != 0 {
		t.Fatalf("presence frames = %v, want none", names)
	}
	if refreshes, _ := pres.snapshot(); refreshes == 0 {
		t.Fatal("Refresh was never called")
	}
	if len(conn.wroteSnapshot()) == 0 {
		t.Fatal("identify should still have been written")
	}
}
