// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// budgetClock is hand-cranked: the budget's whole job is measured in
// minutes and hours, and a test that slept through a 24h window would not be
// a test.
type budgetClock struct{ t time.Time }

func (c *budgetClock) now() time.Time { return c.t }

func (c *budgetClock) advance(d time.Duration) { c.t = c.t.Add(d) }

// testBudget is the production schedule with the ceiling shrunk to something
// a test can reach. The durations stay real so the assertions below are
// assertions about the shipped numbers.
func testBudget(ceiling int) (*connectBudget, *budgetClock) {
	c := &budgetClock{t: time.Unix(1_700_000_000, 0)}
	sched := defaultBudgetSchedule()
	sched.ceiling = ceiling
	return &connectBudget{sched: sched, now: c.now}, c
}

// Rule (a): one connect per minConnectInterval, which is Discord's own
// published IDENTIFY rate. This is the rule that would have capped the
// 2026-09-05 loop at 720 attempts/hour instead of ~1800.
func TestBudgetSpacesConnects(t *testing.T) {
	b, c := testBudget(dailyConnectCeiling)

	if d := b.delay(); d != 0 {
		t.Fatalf("first connect delay = %s, want 0", d)
	}
	b.note()
	if d := b.delay(); d != minConnectInterval {
		t.Fatalf("delay right after a connect = %s, want %s", d, minConnectInterval)
	}
	c.advance(minConnectInterval - time.Second)
	if d := b.delay(); d != time.Second {
		t.Fatalf("delay = %s, want the remaining second", d)
	}
	c.advance(2 * time.Second)
	if d := b.delay(); d != 0 {
		t.Fatalf("delay once the interval passed = %s, want 0", d)
	}
}

// Rule (b): five consecutive sessions each dying young is a flap, and a flap
// escalates the wait to flapWait. One healthy session clears it -- a bot that
// reconnects cleanly once an hour must never be treated as flapping.
func TestBudgetEscalatesOnFlapAndRecovers(t *testing.T) {
	b, _ := testBudget(dailyConnectCeiling)
	b.note()

	for i := range flapStreak - 1 {
		if st := b.record(time.Second); st.Flapping {
			t.Fatalf("flagged flapping after %d short sessions, want %d", i+1, flapStreak)
		}
	}
	st := b.record(time.Second)
	if !st.Flapping {
		t.Fatalf("state after %d short sessions = %+v, want flapping", flapStreak, st)
	}
	if d := b.delay(); d != flapWait {
		t.Fatalf("delay while flapping = %s, want %s", d, flapWait)
	}

	// A session that survived flapMinUptime is evidence the loop ended.
	if st := b.record(flapMinUptime); st.Flapping {
		t.Fatalf("state after a healthy session = %+v, want flapping cleared", st)
	}
	if d := b.delay(); d != minConnectInterval {
		t.Fatalf("delay after recovery = %s, want the ordinary floor %s", d, minConnectInterval)
	}
}

// Rule (c): the hard ceiling parks the next connect until the oldest attempt
// ages out of the rolling window -- a park that ends on its own, unlike a
// fatal close.
func TestBudgetParksAtTheCeilingUntilTheWindowFrees(t *testing.T) {
	const ceiling = 3
	b, c := testBudget(ceiling)

	for range ceiling {
		b.note()
		c.advance(time.Hour)
	}
	st := b.snapshot()
	if !st.AtCeiling || st.Connects != ceiling {
		t.Fatalf("state at the ceiling = %+v", st)
	}
	// The first attempt was 3h ago, so the window frees 21h from now.
	if d := b.delay(); d != connectWindow-3*time.Hour {
		t.Fatalf("ceiling delay = %s, want %s", d, connectWindow-3*time.Hour)
	}

	c.advance(connectWindow - 3*time.Hour)
	st = b.snapshot()
	if st.AtCeiling || st.Connects != ceiling-1 {
		t.Fatalf("state once the oldest attempt aged out = %+v", st)
	}
	if d := b.delay(); d != 0 {
		t.Fatalf("delay after the window freed = %s, want 0", d)
	}
}

// The ceiling counts attempts, not successful sessions: a dial that never
// completes a handshake spends the same Identify allowance as one that does.
func TestBudgetCountsFailedAttempts(t *testing.T) {
	b, c := testBudget(dailyConnectCeiling)
	for range 4 {
		b.note()
		c.advance(minConnectInterval)
		b.record(0) // never reached READY
	}
	if st := b.snapshot(); st.Connects != 4 {
		t.Fatalf("connects = %d, want 4", st.Connects)
	}
}

// The budget floors the reconnect backoff rather than replacing it: whichever
// says "wait longer" wins.
func TestRunHonoursTheConnectFloor(t *testing.T) {
	dial, dials := dialCounter(4000)
	// Backoff alone dials again within backoffMin (1s), so without the floor
	// this window holds at least two attempts.
	ctx, cancel := context.WithTimeout(context.Background(), 1600*time.Millisecond)
	defer cancel()

	st := &recStatus{}
	sess := Session{Token: "bot-token", Dial: dial, Status: st}
	if err := sess.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run err = %v, want the context error", err)
	}
	if got := dials(); got != 1 {
		t.Fatalf("dials = %d in %s, want exactly 1: the connect floor is %s", got, 1600*time.Millisecond, minConnectInterval)
	}
	if states := st.budgetStates(); len(states) != 1 || states[0].Connects != 1 {
		t.Fatalf("budget states = %+v, want one connect published", states)
	}
}

// The flap verdict has to reach both the status key and the log: "offline"
// and "offline, and this process is deliberately down to one attempt every
// five minutes" need different answers from whoever is looking, and the log
// line is what names the close code that caused it.
func TestFlappingIsPublishedLoggedAndWaited(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	st := &recStatus{}
	sess := Session{Status: st, Log: zap.New(core)}
	bud, _ := testBudget(dailyConnectCeiling)
	// draw = identity: the backoff schedule is asserted elsewhere, and here
	// it must be the loser -- the budget's escalation is what decides.
	rc := &reconnect{draw: func(d time.Duration) time.Duration { return d }}

	var wait time.Duration
	for range flapStreak {
		bud.note()
		wait = sess.afterSocket(context.Background(), budgetInputs{bud: bud, rc: rc},
			sessionEnd{up: time.Second, code: 4000, err: errors.New("socket died")})
	}

	wantFlapStates(t, st.budgetStates())
	if wait != flapWait {
		t.Fatalf("wait = %s, want the escalated %s (the budget must outrank backoff)", wait, flapWait)
	}
	wantFlapLog(t, logs, 4000, "socket died")
}

// wantFlapStates asserts the published run: one state per short session, with
// the verdict landing only on the flapStreak-th. Publishing it earlier is what
// would make an ordinary hourly reconnect read as a flap on the status key.
func wantFlapStates(t *testing.T, states []Budget) {
	t.Helper()
	if len(states) != flapStreak {
		t.Fatalf("budget states = %d, want %d", len(states), flapStreak)
	}
	if states[flapStreak-2].Flapping {
		t.Fatalf("flapping after %d short sessions, want it to take %d", flapStreak-1, flapStreak)
	}
	last := states[flapStreak-1]
	if !last.Flapping {
		t.Fatalf("final state = %+v, want flapping", last)
	}
	if last.Connects != flapStreak {
		t.Fatalf("final state connects = %d, want %d", last.Connects, flapStreak)
	}
}

// wantFlapLog asserts the one ERROR line the verdict owes an operator, and
// that it names the close code and error of the session that tipped it: the
// status key says "flapping", only the log says what kept killing the socket.
func wantFlapLog(t *testing.T, logs *observer.ObservedLogs, code int64, cause string) {
	t.Helper()
	errs := logs.FilterLevelExact(zapcore.ErrorLevel).All()
	if len(errs) != 1 {
		t.Fatalf("error logs = %v, want exactly one", errs)
	}
	if errs[0].Message != "gateway flapping" {
		t.Fatalf("error log = %q, want \"gateway flapping\"", errs[0].Message)
	}
	fields := errs[0].ContextMap()
	if fields["close_code"] != code {
		t.Fatalf("flap log close_code = %v, want %d", fields["close_code"], code)
	}
	if fields["error"] != cause {
		t.Fatalf("flap log error = %v, want %q", fields["error"], cause)
	}
}

// At the ceiling the loop parks the way a fatal close does, with its own
// ERROR line -- the difference being that this park ends when the window
// frees rather than when a human rotates a secret.
func TestCeilingParksWithItsOwnErrorLine(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	sess := Session{Log: zap.New(core)}
	bud, c := testBudget(2)
	rc := &reconnect{draw: func(d time.Duration) time.Duration { return d }}

	bud.note()
	c.advance(time.Hour)
	bud.note()
	wait := sess.afterSocket(context.Background(), budgetInputs{bud: bud, rc: rc},
		sessionEnd{up: 10 * time.Minute, code: 4000})

	if wait != connectWindow-time.Hour {
		t.Fatalf("wait = %s, want the window to free in %s", wait, connectWindow-time.Hour)
	}
	errs := logs.FilterLevelExact(zapcore.ErrorLevel).All()
	if len(errs) != 1 || errs[0].ContextMap()["connects_in_window"] != int64(2) {
		t.Fatalf("error logs = %v, want one naming the connect count", errs)
	}
}

// The budget must never turn a fatal close into a wait: a 4004 parks
// immediately, budget or not.
func TestBudgetDoesNotDelayTheFatalPath(t *testing.T) {
	dial, dials := dialCounter(ddiscord.CloseAuthenticationFailed)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	st := &recStatus{}
	sess := Session{Token: "bot-token", Dial: dial, Status: st}
	_ = sess.Run(ctx)

	if got := dials(); got != 1 {
		t.Fatalf("dials = %d, want 1", got)
	}
	if states := st.budgetStates(); len(states) != 0 {
		t.Fatalf("budget states = %+v, want none: a fatal close parks before any reconnect scheduling", states)
	}
}
