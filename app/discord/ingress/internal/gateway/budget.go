// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package gateway

import (
	"context"
	"slices"
	"sync"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"

	"go.uber.org/zap"
)

// Discord resets the bot token past 1000 identifies a day: never loosen these limits.
const (
	minConnectInterval  = 5 * time.Second
	flapMinUptime       = 60 * time.Second
	flapStreak          = 5
	flapWait            = 5 * time.Minute
	dailyConnectCeiling = 800
	connectWindow       = 24 * time.Hour
)

// Add must accumulate, never overwrite: pods sharing one token must sum their attempts.
type ConnectLog interface {
	Load(ctx context.Context, since time.Time) ([]time.Time, error)
	Add(ctx context.Context, at time.Time) error
}

const storeTimeout = 2 * time.Second

type budgetSchedule struct {
	minInterval time.Duration
	flapUptime  time.Duration
	flapStreak  int
	flapWait    time.Duration
	ceiling     int
	window      time.Duration
}

func defaultBudgetSchedule() budgetSchedule {
	return budgetSchedule{
		minInterval: minConnectInterval,
		flapUptime:  flapMinUptime,
		flapStreak:  flapStreak,
		flapWait:    flapWait,
		ceiling:     dailyConnectCeiling,
		window:      connectWindow,
	}
}

type Budget struct {
	Flapping  bool
	Connects  int
	AtCeiling bool
	ParkUntil time.Time
}

type connectBudget struct {
	mu       sync.Mutex
	sched    budgetSchedule
	now      func() time.Time
	store    ConnectLog
	log      *zap.Logger
	warnedAt time.Time

	last      time.Time
	shortRuns int
	flapping  bool
	attempts  []time.Time
}

func newConnectBudget(store ConnectLog, log *zap.Logger) *connectBudget {
	b := &connectBudget{sched: defaultBudgetSchedule(), now: time.Now, store: store, log: log}
	b.reload()
	return b
}

func (b *connectBudget) reload() {
	if b.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	now := b.now()
	seen, err := b.store.Load(ctx, now.Add(-b.sched.window))
	if err != nil {
		b.degraded(err)
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempts = mergeWindow(b.attempts, seen)
	b.prune(now)
}

func mergeWindow(local, shared []time.Time) []time.Time {
	unmatched := countByMilli(local)
	for _, at := range shared {
		unmatched[at.UnixMilli()]--
	}
	out := append([]time.Time(nil), shared...)
	for _, at := range local {
		ms := at.UnixMilli()
		if unmatched[ms] <= 0 {
			continue
		}
		unmatched[ms]--
		out = append(out, at)
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return out
}

func countByMilli(stamps []time.Time) map[int64]int {
	n := make(map[int64]int, len(stamps))
	for _, at := range stamps {
		n[at.UnixMilli()]++
	}
	return n
}

func (b *connectBudget) persist(at time.Time) {
	if b.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), storeTimeout)
	defer cancel()
	if err := b.store.Add(ctx, at); err != nil {
		b.degraded(err)
	}
}

const degradedWarnEvery = 5 * time.Minute

func (b *connectBudget) degraded(err error) {
	if !b.warningDue() {
		return
	}
	b.logger().Warn("discord connect budget is not persisted; counting this process only",
		zap.String("key", ddiscord.BotConnectsKey), zap.Error(err))
}

func (b *connectBudget) warningDue() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if !b.warnedAt.IsZero() && now.Sub(b.warnedAt) < degradedWarnEvery {
		return false
	}
	b.warnedAt = now
	return true
}

func (b *connectBudget) logger() *zap.Logger {
	if b.log != nil {
		return b.log
	}
	return zap.NewNop()
}

func (b *connectBudget) note() {
	now := b.now()
	b.mu.Lock()
	b.prune(now)
	b.last = now
	b.attempts = append(b.attempts, now)
	b.mu.Unlock()
	b.persist(now)
}

func (b *connectBudget) record(up time.Duration) Budget {
	b.reload()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(b.now())
	if up < b.sched.flapUptime {
		b.shortRuns++
	} else {
		b.shortRuns = 0
	}
	b.flapping = b.shortRuns >= b.sched.flapStreak
	return b.state()
}

func (b *connectBudget) delay() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	b.prune(now)
	d := b.spacing(now)
	if wait := b.ceilingWait(now); wait > d {
		d = wait
	}
	return d
}

func (b *connectBudget) snapshot() Budget {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(b.now())
	return b.state()
}

func (b *connectBudget) state() Budget {
	st := Budget{
		Flapping:  b.flapping,
		Connects:  len(b.attempts),
		AtCeiling: len(b.attempts) >= b.sched.ceiling,
	}
	st.ParkUntil = b.parkUntil(st)
	return st
}

func (b *connectBudget) parkUntil(st Budget) time.Time {
	if st.AtCeiling && len(b.attempts) > 0 {
		return b.attempts[0].Add(b.sched.window)
	}
	if st.Flapping && !b.last.IsZero() {
		return b.last.Add(b.sched.flapWait)
	}
	return time.Time{}
}

func (b *connectBudget) spacing(now time.Time) time.Duration {
	if b.last.IsZero() {
		return 0
	}
	gap := b.sched.minInterval
	if b.flapping {
		gap = b.sched.flapWait
	}
	return timeRemaining(b.last.Add(gap), now)
}

func (b *connectBudget) ceilingWait(now time.Time) time.Duration {
	if len(b.attempts) < b.sched.ceiling {
		return 0
	}
	return timeRemaining(b.attempts[0].Add(b.sched.window), now)
}

func (b *connectBudget) prune(now time.Time) {
	cut := now.Add(-b.sched.window)
	i := 0
	for i < len(b.attempts) && !b.attempts[i].After(cut) {
		i++
	}
	b.attempts = b.attempts[i:]
}

func timeRemaining(deadline, now time.Time) time.Duration {
	if d := deadline.Sub(now); d > 0 {
		return d
	}
	return 0
}
