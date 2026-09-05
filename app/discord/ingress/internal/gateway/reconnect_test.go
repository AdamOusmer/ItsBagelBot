// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
)

func TestBackoffCeilingGrowsAndCaps(t *testing.T) {
	want := []time.Duration{
		time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second, 60 * time.Second, 60 * time.Second,
	}
	for attempt, w := range want {
		if got := backoffCeiling(attempt); got != w {
			t.Fatalf("backoffCeiling(%d) = %s, want %s", attempt, got, w)
		}
	}
	// A gateway can stay unreachable for days; the shift must not wrap.
	if got := backoffCeiling(200); got != backoffMax {
		t.Fatalf("backoffCeiling(200) = %s, want %s", got, backoffMax)
	}
}

func TestFullJitterStaysInBounds(t *testing.T) {
	const ceiling = 8 * time.Second
	seen := map[time.Duration]bool{}
	for range 500 {
		d := fullJitter(ceiling)
		if d < 0 || d > ceiling {
			t.Fatalf("fullJitter drew %s, outside [0, %s]", d, ceiling)
		}
		seen[d] = true
	}
	if len(seen) < 2 {
		t.Fatal("fullJitter drew one value 500 times; it is not jittering")
	}
	if got := fullJitter(0); got != 0 {
		t.Fatalf("fullJitter(0) = %s, want 0", got)
	}
}

func TestReconnectSchedulesAndResetsAfterStableSocket(t *testing.T) {
	// draw = identity so the schedule itself is asserted, not a draw from it.
	rc := &reconnect{draw: func(d time.Duration) time.Duration { return d }}
	for _, want := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
		if got := rc.next(time.Millisecond); got != want {
			t.Fatalf("next() = %s, want %s", got, want)
		}
	}
	if got := rc.next(stableFor); got != time.Second {
		t.Fatalf("next() after a %s socket = %s, want the schedule reset to %s", stableFor, got, time.Second)
	}
	if got := rc.next(time.Millisecond); got != 2*time.Second {
		t.Fatalf("next() after the reset = %s, want %s", got, 2*time.Second)
	}
}

// recStatus records the lifecycle callbacks Session makes.
type recStatus struct {
	mu       sync.Mutex
	ups      []Up
	down     []Down
	budgets  []Budget
	eventHit int
}

func (r *recStatus) Up(_ context.Context, up Up) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ups = append(r.ups, up)
}

func (r *recStatus) Down(_ context.Context, d Down) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.down = append(r.down, d)
}

func (r *recStatus) Event(context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.eventHit++
}

func (r *recStatus) Budget(_ context.Context, b Budget) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.budgets = append(r.budgets, b)
}

func (r *recStatus) downs() []Down {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Down(nil), r.down...)
}

func (r *recStatus) budgetStates() []Budget {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Budget(nil), r.budgets...)
}

func (r *recStatus) events() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.eventHit
}

// openBudget is a connect budget that holds nothing back. Tests that assert
// the *backoff* schedule install it so the 5s connect floor (budget.go) does
// not decide their timing for them; tests about the budget itself build one
// with a real schedule and a fake clock.
func openBudget() *connectBudget {
	return &connectBudget{now: time.Now, sched: budgetSchedule{ceiling: 1 << 30, window: connectWindow}}
}

// dialCounter builds a Dial that hands out a socket dying with code every
// time, counting how many times Run asked for one.
func dialCounter(code int) (Dial, func() int) {
	var mu sync.Mutex
	n := 0
	dial := func(context.Context, string) (Conn, error) {
		mu.Lock()
		n++
		mu.Unlock()
		return &scriptedConn{readErr: errors.New("websocket closed"), closeCode: code}, nil
	}
	return dial, func() int {
		mu.Lock()
		defer mu.Unlock()
		return n
	}
}

func TestFatalCloseStopsReconnecting(t *testing.T) {
	dial, dials := dialCounter(ddiscord.CloseDisallowedIntents)
	st := &recStatus{}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	sess := Session{Token: "bot-token", Dial: dial, Status: st}
	if err := sess.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run err = %v, want the context error (Run must park, not return early)", err)
	}

	// One dial, not a loop: 4014 is not fixable by dialing again, and the
	// old flat-2s loop would have made ~75 attempts in this window.
	if got := dials(); got != 1 {
		t.Fatalf("dials = %d, want exactly 1 after a fatal close", got)
	}
	downs := st.downs()
	if len(downs) != 1 || !downs[0].Fatal || downs[0].Code != ddiscord.CloseDisallowedIntents {
		t.Fatalf("Down = %+v, want one fatal 4014", downs)
	}
}

func TestNonFatalCloseKeepsReconnecting(t *testing.T) {
	dial, dials := dialCounter(4000)
	// The first backoff ceiling is backoffMin, so a second dial is due
	// within 1s no matter what the jitter draws; 1.5s makes that certain
	// without depending on the draw.
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	st := &recStatus{}
	// openBudget: this test is about the backoff schedule, and the real
	// connect budget's 5s floor would make one dial the correct answer.
	sess := Session{Token: "bot-token", Dial: dial, Status: st, budget: openBudget()}
	if err := sess.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run err = %v, want the context error", err)
	}
	if got := dials(); got < 2 {
		t.Fatalf("dials = %d, want at least 2: 4000 is a reconnectable close", got)
	}
	for _, d := range st.downs() {
		if d.Fatal {
			t.Fatalf("Down %+v marked fatal; 4000 is not in the fatal set", d)
		}
	}
}
