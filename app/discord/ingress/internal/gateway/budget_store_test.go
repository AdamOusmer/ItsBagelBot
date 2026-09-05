// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// fakeConnectLog stands in for the Valkey sorted set: an ordered list of
// attempt stamps, pruned on read exactly as botstatus.ConnectLog prunes with
// ZREMRANGEBYSCORE. fail makes every call error, which is the Valkey-down
// path.
type fakeConnectLog struct {
	mu   sync.Mutex
	seen []time.Time
	fail error
}

func (f *fakeConnectLog) Load(_ context.Context, since time.Time) ([]time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	kept := make([]time.Time, 0, len(f.seen))
	for _, at := range f.seen {
		if !at.Before(since) {
			kept = append(kept, at)
		}
	}
	f.seen = kept
	return append([]time.Time(nil), kept...), nil
}

func (f *fakeConnectLog) Add(_ context.Context, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.seen = append(f.seen, at)
	return nil
}

// storedBudget is testBudget wired to a store, with the same hand-cranked
// clock so a 24h window can be asserted without waiting one.
func storedBudget(store ConnectLog, log *zap.Logger, c *budgetClock) *connectBudget {
	b := &connectBudget{sched: defaultBudgetSchedule(), now: c.now, store: store, log: log}
	b.reload()
	return b
}

// The window has to outlive the process. The 2026-09-05 loop was a
// crash-loop: every restart handed the old budget a clean 800, which is 800
// attempts per crash against a limit Discord counts per token per day.
func TestBudgetWindowSurvivesARestart(t *testing.T) {
	store := &fakeConnectLog{}
	c := &budgetClock{t: time.Unix(1_700_000_000, 0)}

	first := storedBudget(store, nil, c)
	for range 3 {
		first.note()
		c.advance(minConnectInterval)
	}

	// A new process, the same shared window.
	restarted := storedBudget(store, nil, c)
	if st := restarted.snapshot(); st.Connects != 3 {
		t.Fatalf("connects after a restart = %d, want the 3 the old process spent", st.Connects)
	}

	restarted.note()
	if st := restarted.snapshot(); st.Connects != 4 {
		t.Fatalf("connects = %d, want the restarted process to keep counting up", st.Connects)
	}
}

// Attempts that have aged out of the window are dropped on the way back in,
// so a pod restarting after a quiet day does not inherit yesterday's spend.
func TestRestartDropsAttemptsOlderThanTheWindow(t *testing.T) {
	store := &fakeConnectLog{}
	c := &budgetClock{t: time.Unix(1_700_000_000, 0)}

	old := storedBudget(store, nil, c)
	old.note()
	c.advance(connectWindow + time.Minute)
	old.note()

	if st := storedBudget(store, nil, c).snapshot(); st.Connects != 1 {
		t.Fatalf("connects = %d, want only the attempt still inside the window", st.Connects)
	}
}

// Valkey being unreachable must never block a connect: the budget degrades
// to what shipped before it was persisted, counting this process only, and
// says so exactly once. A WARN per attempt would bury the gateway's own log.
func TestStoreFailureDegradesToMemoryWithOneWarning(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	store := &fakeConnectLog{fail: errors.New("valkey: connection refused")}
	c := &budgetClock{t: time.Unix(1_700_000_000, 0)}

	b := storedBudget(store, zap.New(core), c)
	for range 3 {
		b.note()
		c.advance(minConnectInterval)
		b.record(time.Second)
	}

	if st := b.snapshot(); st.Connects != 3 {
		t.Fatalf("connects = %d, want the in-memory window to keep working", st.Connects)
	}
	warns := logs.FilterLevelExact(zapcore.WarnLevel).All()
	if len(warns) != 1 {
		t.Fatalf("warn logs = %d, want exactly one across a boot, 3 notes and 3 records", len(warns))
	}
}

// record folds in whatever other pods spent before it publishes a verdict:
// two ingress replicas by accident is the exact case this budget exists to
// survive, and they share one token's allowance.
func TestRecordReadsBackTheSharedWindow(t *testing.T) {
	store := &fakeConnectLog{}
	c := &budgetClock{t: time.Unix(1_700_000_000, 0)}
	b := storedBudget(store, nil, c)
	b.sched.ceiling = 3

	b.note()
	// A second pod spends the rest of the allowance behind this one's back.
	for range 2 {
		if err := store.Add(context.Background(), c.t); err != nil {
			t.Fatal(err)
		}
	}

	st := b.record(time.Minute)
	if st.Connects != 3 || !st.AtCeiling {
		t.Fatalf("state = %+v, want the other pod's attempts counted and the ceiling hit", st)
	}
	if st.ParkUntil.IsZero() {
		t.Fatal("a spent ceiling must publish when it frees")
	}
}

// runBudget is a schedule whose only live rule is the ceiling: the identify
// floor is shrunk to nothing and no session counts as short, so a Run-level
// assertion about dials stopping can only be the ceiling's doing.
func runBudget(ceiling int) *connectBudget {
	sched := defaultBudgetSchedule()
	sched.ceiling = ceiling
	sched.minInterval = time.Millisecond
	sched.flapUptime = 0
	return &connectBudget{sched: sched, now: time.Now}
}

// The ceiling has to stop the loop, not just describe it. Without it the
// backoff schedule dials again within backoffMin, so this window holds
// several attempts.
func TestRunStopsDiallingAtTheCeiling(t *testing.T) {
	dial, dials := dialCounter(4000)
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	st := &recStatus{}
	sess := Session{Token: "bot-token", Dial: dial, Status: st, budget: runBudget(1)}
	if err := sess.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run err = %v, want the context error", err)
	}

	if got := dials(); got != 1 {
		t.Fatalf("dials = %d, want exactly 1: the ceiling is 1", got)
	}
	states := st.budgetStates()
	if len(states) != 1 || !states[len(states)-1].AtCeiling {
		t.Fatalf("budget states = %+v, want the ceiling published", states)
	}
	if states[0].ParkUntil.IsZero() {
		t.Fatal("the published state must say when the window frees")
	}
}

// A resume is still a socket. Discord counts the connection, not the opcode
// that follows it, so a session that RESUMEs spends the budget exactly like
// one that identifies -- a loop of cheap resumes would otherwise be
// invisible to the ceiling that exists to bound it.
func TestResumedReconnectSpendsTheBudget(t *testing.T) {
	dial, dials := readyThenResumedDial(t)
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	st := &recStatus{}
	sess := Session{Token: "bot-token", Dial: dial, Status: st, budget: runBudget(dailyConnectCeiling)}
	if err := sess.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run err = %v, want the context error", err)
	}

	// Not an exact dial count: once the two scripted sockets are spent the
	// loop keeps dialling parked ones on a jittered backoff, and how many it
	// fits before the deadline is a coin flip.
	if got := dials(); got < 2 {
		t.Fatalf("dials = %d, want at least 2 (a READY socket then a RESUMED one)", got)
	}
	ups := st.upStates()
	if len(ups) != 2 || ups[0].Resumed || !ups[1].Resumed {
		t.Fatalf("ups = %+v, want a READY then a RESUMED", ups)
	}
	states := st.budgetStates()
	if len(states) < 2 || states[1].Connects != 2 {
		t.Fatalf("budget states = %+v, want the resumed socket counted too", states)
	}
}

// readyThenResumedDial scripts a socket that reaches READY and dies, then
// one that RESUMEs and dies, then sockets that simply park -- so the dial
// count settles at 2 rather than racing the test's own deadline.
func readyThenResumedDial(t *testing.T) (Dial, func() int) {
	t.Helper()
	hello, err := fastHello()
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	scripts := [][][]byte{
		{hello, dispatchPacket(t, eventReady, readyData{SessionID: "sess-1", ResumeGatewayURL: "wss://resume"})},
		{hello, dispatchPacket(t, eventResumed, struct{}{})},
	}
	var mu sync.Mutex
	n := 0
	dial := func(context.Context, string) (Conn, error) {
		mu.Lock()
		defer mu.Unlock()
		n++
		if n > len(scripts) {
			// No readErr and no closed channel: Read parks until ctx ends.
			return &scriptedConn{}, nil
		}
		return &scriptedConn{reads: scripts[n-1], readErr: errors.New("websocket closed")}, nil
	}
	return dial, func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
}

func dispatchPacket(t *testing.T, name string, data any) []byte {
	t.Helper()
	d, err := codec.Marshal(data)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	raw, err := codec.Marshal(packet{Op: opDispatch, T: name, D: d})
	if err != nil {
		t.Fatalf("marshal %s packet: %v", name, err)
	}
	return raw
}

func (r *recStatus) upStates() []Up {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Up(nil), r.ups...)
}
