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
	_ = sess.oneSocket(ctx, "ws://example", &resumeState{})
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
	_ = sess.oneSocket(ctx, "ws://example", &resumeState{})

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
	_ = sess.oneSocket(ctx, "ws://example", &resumeState{})

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
	err := sess.oneSocket(ctx, "ws://example", &resumeState{})
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

// opOf returns the op codes written to the socket, in order, so a test can
// assert Identify versus Resume without depending on the rest of the frame.
func opsWritten(t *testing.T, frames [][]byte) []int {
	t.Helper()
	var ops []int
	for _, raw := range frames {
		var pkt packet
		if err := codec.Unmarshal(raw, &pkt); err != nil {
			continue
		}
		ops = append(ops, pkt.Op)
	}
	return ops
}

func helloFrame(t *testing.T) []byte {
	t.Helper()
	raw, err := codec.Marshal(packet{Op: opHello, D: mustRaw(t, helloData{HeartbeatInterval: 50000})})
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	return raw
}

// firstOpIs reports whether ops has an entry and its first one is want. Both
// TestSessionIdentifiesWithoutAStoredSession and TestSessionResumesAfterReady
// check this, so it is named once here instead of each repeating the
// length-guard-plus-comparison inline.
func firstOpIs(ops []int, want int) bool {
	return len(ops) > 0 && ops[0] == want
}

// A first connect has no session to continue, so it must Identify.
func TestSessionIdentifiesWithoutAStoredSession(t *testing.T) {
	conn := &scriptedConn{reads: [][]byte{helloFrame(t)}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	sess := Session{Token: "t", Dial: func(context.Context, string) (Conn, error) { return conn, nil }, Handle: &recHandler{}}
	_ = sess.oneSocket(ctx, "ws://x", &resumeState{})
	if ops := opsWritten(t, conn.wroteSnapshot()); !firstOpIs(ops, opIdentify) {
		t.Fatalf("first frame ops = %v, want Identify (%d) first", ops, opIdentify)
	}
}

// The reason this whole mechanism exists: after READY records a session, a
// reconnect must Resume so Discord replays what it buffered during the gap,
// rather than Identify and discard it.
func TestSessionResumesAfterReady(t *testing.T) {
	ready, err := codec.Marshal(packet{
		Op: opDispatch, T: eventReady, S: intPtr(7),
		D: mustRaw(t, readyData{SessionID: "sess-1", ResumeGatewayURL: "ws://resume"}),
	})
	if err != nil {
		t.Fatalf("marshal ready: %v", err)
	}
	st := &resumeState{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	first := &scriptedConn{reads: [][]byte{helloFrame(t), ready}}
	sess := Session{Token: "t", Dial: func(context.Context, string) (Conn, error) { return first, nil }, Handle: &recHandler{}}
	_ = sess.oneSocket(ctx, "ws://x", st)

	sessionID, resumeURL, ok := st.resumable()
	if !matchesResumeState(sessionID, resumeURL, ok, "sess-1", "ws://resume") {
		t.Fatalf("resume state = (%q, %q, %t), want the ids from READY", sessionID, resumeURL, ok)
	}

	second := &scriptedConn{reads: [][]byte{helloFrame(t)}}
	sess.Dial = func(context.Context, string) (Conn, error) { return second, nil }
	_ = sess.oneSocket(ctx, resumeURL, st)
	if ops := opsWritten(t, second.wroteSnapshot()); !firstOpIs(ops, opResume) {
		t.Fatalf("reconnect ops = %v, want Resume (%d) first", ops, opResume)
	}
}

// matchesResumeState reports whether a resumeState.resumable() triple is
// exactly the resumable session recorded from one READY, naming what the
// three-value comparison in TestSessionResumesAfterReady means.
func matchesResumeState(sessionID, resumeURL string, ok bool, wantSessionID, wantResumeURL string) bool {
	return ok && sessionID == wantSessionID && resumeURL == wantResumeURL
}

// INVALID_SESSION with d:false means the session is gone. Keeping it would
// retry a resume Discord has already refused, looping while events pile up.
func TestSessionInvalidSessionNotResumableClearsState(t *testing.T) {
	st := &resumeState{}
	st.ready("sess-1", "ws://resume")
	st.note(intPtr(4))
	sess := Session{Token: "t"}
	if err := sess.onInvalidSession(packet{Op: opInvalidSession, D: mustRaw(t, false)}, st); err == nil {
		t.Fatal("invalid session must end the socket")
	}
	if _, _, ok := st.resumable(); ok {
		t.Fatal("non-resumable invalid session left the session id in place")
	}
}

// d:true keeps the session so the next socket resumes into it.
func TestSessionInvalidSessionResumableKeepsState(t *testing.T) {
	st := &resumeState{}
	st.ready("sess-1", "ws://resume")
	sess := Session{Token: "t"}
	_ = sess.onInvalidSession(packet{Op: opInvalidSession, D: mustRaw(t, true)}, st)
	if _, _, ok := st.resumable(); !ok {
		t.Fatal("resumable invalid session discarded the session id")
	}
}

// The heartbeat must report the last sequence seen. A permanent null tells
// Discord this client has received nothing, defeating its own missed-event
// detection even while the socket is healthy.
func TestResumeStateTracksSequence(t *testing.T) {
	st := &resumeState{}
	if st.sequence() != nil {
		t.Fatal("fresh state reported a sequence")
	}
	st.note(intPtr(3))
	st.note(nil) // non-dispatch frames carry no s and must not clear it
	got := st.sequence()
	if got == nil || *got != 3 {
		t.Fatalf("sequence = %v, want 3", got)
	}
	st.invalidate()
	if st.sequence() != nil {
		t.Fatal("invalidate left a sequence from the dead session")
	}
}

func intPtr(v int) *int { return &v }
