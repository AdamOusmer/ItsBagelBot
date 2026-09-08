// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/pkg/codec"

	"github.com/coder/websocket"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type scriptedConn struct {
	mu    sync.Mutex
	reads [][]byte
	wrote [][]byte
	// readErr, when set, is what Read returns once the scripted reads run
	// out, instead of blocking on ctx. closeCode is the code CloseCode then
	// reports for it, and closeReason the text CloseReason reports --
	// together they script a socket dying the way Discord kills one, close
	// frame text included.
	readErr     error
	closeCode   int
	closeReason string
	// writeErr, when set, fails Write with it. That is how Discord's fatal
	// close frames most often surface in production: on the heartbeat's
	// write, not on the pump's read. writeErrAfter is how many writes
	// succeed first, so a test can let the Identify through and fail only
	// the heartbeat that follows it.
	writeErr      error
	writeErrAfter int
	// closed, when non-nil, is closed by Close and unblocks a Read that is
	// parked waiting for a frame -- the way a real socket behaves when a
	// writer goroutine closes it out from under the pump. Nil (the literal
	// most tests build) blocks forever on that select arm, which is the
	// old behaviour.
	closed    chan struct{}
	closeOnce sync.Once
}

func (s *scriptedConn) Read(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	if len(s.reads) == 0 {
		err := s.readErr
		closed := s.closed
		s.mu.Unlock()
		if err != nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-closed:
			// Deliberately codeless and generic: this is what the pump sees
			// when a writer closed the socket, and the point of
			// socket.firstError is that this must not be the error the
			// caller ends up with.
			return nil, errors.New("use of closed network connection")
		}
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
	var err error
	if len(s.wrote) > s.writeErrAfter {
		err = s.writeErr
	}
	s.mu.Unlock()
	return err
}

func (s *scriptedConn) Close() error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed != nil {
		s.closeOnce.Do(func() { close(closed) })
	}
	return nil
}

// CloseCode mirrors wsConn: a real close frame carries its own code, and
// only an error that is not one falls back to whatever the script attached.
// Delegating to websocket.CloseStatus here is what makes the write-error
// test exercise the real routing rather than a fake that answers 4004 to
// anything.
func (s *scriptedConn) CloseCode(err error) int {
	if code := websocket.CloseStatus(err); code >= 0 {
		return int(code)
	}
	return s.closeCode
}

// CloseReason mirrors wsConn the same way CloseCode does: a real close frame
// carries its own text, and only an error that is not one falls back to the
// script.
func (s *scriptedConn) CloseReason(err error) string {
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return ce.Reason
	}
	return s.closeReason
}

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
	end := sess.oneSocket(ctx, "ws://example", &resumeState{})
	if end.err == nil || ctx.Err() == nil {
		t.Fatalf("oneSocket should end on context cancellation, err=%v ctxErr=%v", end.err, ctx.Err())
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
	got := resumeSnapshot{SessionID: sessionID, ResumeURL: resumeURL, OK: ok}
	if !matchesResumeState(got, resumeSnapshot{SessionID: "sess-1", ResumeURL: "ws://resume", OK: true}) {
		t.Fatalf("resume state = (%q, %q, %t), want the ids from READY", sessionID, resumeURL, ok)
	}

	second := &scriptedConn{reads: [][]byte{helloFrame(t)}}
	sess.Dial = func(context.Context, string) (Conn, error) { return second, nil }
	_ = sess.oneSocket(ctx, resumeURL, st)
	if ops := opsWritten(t, second.wroteSnapshot()); !firstOpIs(ops, opResume) {
		t.Fatalf("reconnect ops = %v, want Resume (%d) first", ops, opResume)
	}
}

// resumeSnapshot is a resumeState.resumable() triple, named so
// matchesResumeState can compare "got" against "want" as two values instead
// of five bare parameters (CodeScene: Excess Number of Function Arguments).
// want.OK is always true here -- a comparison against a non-resumable want
// has never been a case this test needs -- but the field stays on the
// shared type rather than a second, narrower one so there is exactly one
// name for "what resumable() returns" in this file.
type resumeSnapshot struct {
	SessionID string
	ResumeURL string
	OK        bool
}

// matchesResumeState reports whether a resumeState.resumable() triple is
// exactly the resumable session recorded from one READY, naming what the
// three-value comparison in TestSessionResumesAfterReady means.
func matchesResumeState(got, want resumeSnapshot) bool {
	return got.OK && got.SessionID == want.SessionID && got.ResumeURL == want.ResumeURL
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

// The ordinary socket-end WARN is the line production actually reads: on
// 2026-09-07 it fired 21,575 times in 24h carrying a close code and an error
// and nothing else, which named neither the fault, nor the process, nor
// whether the resume that preceded it had been honoured. Every one of those
// facts is on the line now, and this is what keeps them there.
func TestSocketEndWarnCarriesTheCloseTelemetry(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	sess := Session{Log: zap.New(core)}
	rc := &reconnect{draw: func(d time.Duration) time.Duration { return d }}
	// A real schedule and a session that outlived flapMinUptime: the WARN is
	// the branch that fires when neither budget rule has anything to say.
	bud, _ := testBudget(dailyConnectCeiling)

	sess.afterSocket(context.Background(), budgetInputs{bud: bud, rc: rc}, sessionEnd{
		up:        flapMinUptime,
		code:      4000,
		reason:    "Session is no longer valid.",
		err:       errors.New("socket died"),
		opened:    openResume,
		sessionID: "sess-1",
		seq:       7,
	})

	warns := logs.FilterLevelExact(zapcore.WarnLevel).All()
	if len(warns) != 1 {
		t.Fatalf("warn logs = %v, want exactly one socket-end line", warns)
	}
	wantFields(t, warns[0].ContextMap(), map[string]any{
		"close_code":   int64(4000),
		"close_reason": "Session is no longer valid.",
		"uptime":       flapMinUptime,
		"opened":       string(openResume),
		"resumed":      false,
		"session_id":   "sess-1",
		"connect_seq":  int64(7),
	})
}

// wantFields asserts every named field of a log line, so the assertions above
// stay one map literal rather than one if per fact.
func wantFields(t *testing.T, got, want map[string]any) {
	t.Helper()
	for name, value := range want {
		if got[name] != value {
			t.Fatalf("log field %s = %v, want %v", name, got[name], value)
		}
	}
}

// A refused resume is the expensive event: Discord answers op 9 d:false and
// the next connect spends one IDENTIFY out of the 1000/day that already got
// this token reset once. It has to be visible as itself, not inferred from a
// close code that reads 4000 either way.
func TestInvalidSessionLogsWhatWasRefused(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	sess := Session{Token: "t", Log: zap.New(core)}
	st := &resumeState{}
	st.ready("sess-1", "ws://resume")
	st.markOpened(openResume)

	_ = sess.onInvalidSession(packet{Op: opInvalidSession, D: mustRaw(t, false)}, st)

	infos := logs.FilterLevelExact(zapcore.InfoLevel).All()
	if len(infos) != 1 {
		t.Fatalf("info logs = %v, want the one op 9 line", infos)
	}
	// session_id survives invalidate() on purpose: the line that says the
	// session died is the one place its id still has to appear.
	wantFields(t, infos[0].ContextMap(), map[string]any{
		"resumable":  false,
		"opened":     string(openResume),
		"session_id": "sess-1",
	})
}

// connect must report what the socket did, not what it was asked to do. A
// resume Discord honoured and one it refused die with the same close code;
// these fields are the only thing that separates them.
func TestConnectReportsHowTheSocketOpened(t *testing.T) {
	st := &resumeState{}
	st.ready("sess-1", "ws://resume")
	conn := &scriptedConn{
		reads:       [][]byte{helloFrame(t), dispatchPacket(t, eventResumed, struct{}{})},
		readErr:     errors.New("websocket closed"),
		closeReason: "Heartbeat ACK not received.",
	}
	sess := Session{Token: "t", Dial: func(context.Context, string) (Conn, error) { return conn, nil }}

	end := sess.connect(context.Background(), "ws://x", st)

	if end.opened != openResume || !end.resumed {
		t.Fatalf("end = %+v, want a resume that landed", end)
	}
	if end.sessionID != "sess-1" {
		t.Fatalf("end session_id = %q, want sess-1", end.sessionID)
	}
	if end.reason != "Heartbeat ACK not received." {
		t.Fatalf("end reason = %q, want the close frame text", end.reason)
	}
}
