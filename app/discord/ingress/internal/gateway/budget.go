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

// The connect budget exists because of a real incident, 2026-09-05: Discord
// reset this bot's token, and the reason they gave was that the bot
// "connected more than 1000 times within a short time period". Two separate
// loops produced it -- a 4004 that reconnected forever because the close
// code arrived on the heartbeat's write and never reached the fatal check
// (see socket.firstError), and a read-limit failure that killed the socket
// immediately after READY (see maxGatewayMessage) -- and both reconnected
// every 2s, which is ~1800 Identify attempts in an hour.
//
// The two underlying bugs are fixed. This is the belt on top, because the
// cost of the next such loop is not a noisy log: it is a revoked token, a
// bot offline for every streamer until a human notices, and a rotation
// through Doppler. Backoff alone was never enough -- it resets on a "stable"
// socket, and both loops looked stable enough often enough to keep the
// schedule pinned at its floor.
//
// The three rules, and why these numbers:
//
//   - minConnectInterval 5s. Discord's documented identify limit is 1 per 5s
//     per bot (the IDENTIFY bucket; 1000/day resets at 00:00 UTC). Anything
//     faster is already a violation, so this is not a tuning knob, it is the
//     published contract. It floors the backoff schedule rather than
//     replacing it.
//   - flapStreak 5 sessions each under flapMinUptime 60s => flapWait 5min.
//     A healthy reconnect (Discord rolling a gateway node) produces one
//     short session, not five in a row; a session that survives a minute has
//     identified, resumed and pumped events. Five is enough to be sure and
//     small enough to catch the loop inside half a minute. 5min is the wait
//     that turns 1800 attempts/hour into 12.
//   - dailyConnectCeiling 800 in a rolling 24h. Discord's hard limit is
//     1000/day; 800 leaves room for the operator's own restarts and rollouts
//     on top of whatever this process spent. A rolling window rather than
//     Discord's UTC-midnight reset because this process cannot see their
//     counter -- a rolling window is the conservative reading of it.
//
// Alternative considered and rejected: making backoff.stableFor much larger
// instead of adding a floor. It fails on the 4004 loop, where every session
// is short but the *first* one after a park is not, so the schedule resets
// and the floor is 1s again. A budget that only ever counts attempts cannot
// be reset by the shape of any single session.
const (
	minConnectInterval  = 5 * time.Second
	flapMinUptime       = 60 * time.Second
	flapStreak          = 5
	flapWait            = 5 * time.Minute
	dailyConnectCeiling = 800
	connectWindow       = 24 * time.Hour
)

// The window outlives the process, and that is the fourth rule.
//
// The 2026-09-05 loop was a crash-loop shape: both underlying bugs killed
// the process, and a budget that starts empty on every boot bounds nothing
// at all -- 800 attempts per pod lifetime is 800 attempts per crash, and
// Discord's counter is per token per day no matter how often the pod came
// back. So the attempt log is written through to Valkey
// (discord.BotConnectsKey) and read back at boot.
//
// Alternative considered and rejected: keeping the count in the status key
// this pod already writes. It is one Valkey round trip fewer, but the key is
// a whole-document overwrite, so two pods -- which is exactly the accident
// this budget guards against -- would clobber each other's counts instead of
// adding to them. A sorted set adds.
//
// Valkey being unreachable degrades to the in-memory window rather than
// blocking a connect: an ingress that cannot reach Valkey must still be able
// to hold a gateway session, and a budget that is merely per-process is
// exactly what shipped before this. It says so at WARN, and keeps saying it
// on a schedule for as long as it is true (see degradedWarnEvery): a notice
// printed once at boot is gone from every window an operator later reads.

// ConnectLog persists the rolling connect window outside this process. The
// gateway package deliberately knows nothing about Valkey -- handing it a
// client would make every gateway test need one, the same split
// botstatus's package doc describes -- so this is the entire surface.
type ConnectLog interface {
	// Load reports every attempt still inside the window, oldest first, and
	// is free to prune the ones before since as it reads.
	Load(ctx context.Context, since time.Time) ([]time.Time, error)
	// Add appends one attempt.
	Add(ctx context.Context, at time.Time) error
}

// storeTimeout bounds one ConnectLog round trip. It matches botstatus's own
// per-write bound and exists for the same reason: these calls sit on the
// connect path, so an unbounded Do against a Valkey that is failing over
// would stall reconnects behind bookkeeping nothing waits on.
const storeTimeout = 2 * time.Second

// budgetSchedule is the four numbers above, in a struct so tests can shrink
// them to milliseconds. Production always uses defaultBudgetSchedule.
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

// Budget is the connect budget as the rest of the fleet sees it. It rides
// out to the status key so a dashboard (and an operator reading the key
// directly) can tell "the bot is offline" from "the bot is offline and this
// process has stopped trying so hard on purpose".
type Budget struct {
	// Flapping is true once flapStreak consecutive sessions each died
	// younger than flapMinUptime.
	Flapping bool
	// Connects is how many sockets this process has opened inside the
	// rolling window.
	Connects int
	// AtCeiling is true when Connects reached the ceiling: the next connect
	// is parked until the oldest attempt ages out of the window.
	AtCeiling bool
	// ParkUntil is when the next connect becomes affordable again, zero
	// while the only thing holding it back is the ordinary minInterval
	// spacing. It rides out to the status key so a dashboard can say when
	// the bot comes back rather than only that it is gone.
	ParkUntil time.Time
}

// connectBudget throttles how often Session may open a socket. It counts
// attempts, not successes: a dial that never completes a handshake spends
// exactly as much of Discord's identify budget as one that does.
type connectBudget struct {
	mu    sync.Mutex
	sched budgetSchedule
	now   func() time.Time
	// store is the shared window, nil when nothing persists it.
	store ConnectLog
	log   *zap.Logger
	// warnedAt is when the degraded-to-memory notice last went out, zero
	// before the first. Valkey failing means it fails on every note and every
	// record, so the notice is paced rather than printed per call; see
	// degradedWarnEvery for why it repeats at all.
	warnedAt time.Time

	// last is when the most recent socket was opened, zero before the first.
	last time.Time
	// shortRuns counts consecutive sessions that died younger than
	// flapUptime. One long session clears it.
	shortRuns int
	flapping  bool
	// attempts holds one timestamp per connect inside the rolling window,
	// oldest first. At the ceiling this is 800 time.Time values (~19 KiB),
	// which is cheaper than any of the alternatives that lose the ability to
	// say exactly when the window frees.
	attempts []time.Time
}

// newConnectBudget builds the budget and seeds its window from store, so a
// pod that just crash-looped starts with whatever its predecessors already
// spent rather than a clean 800.
func newConnectBudget(store ConnectLog, log *zap.Logger) *connectBudget {
	b := &connectBudget{sched: defaultBudgetSchedule(), now: time.Now, store: store, log: log}
	b.reload()
	return b
}

// reload folds the shared window into the in-memory one. A store that answers
// nothing can only ever add history, never delete it.
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

// mergeWindow unions the shared window with the local one, oldest first.
//
// A union, not a replacement. reload used to overwrite the local window with
// whatever the store returned, and the comment above it claimed an outage
// could only lose history. It lost the local half -- the half that bounds the
// ceiling. A store that answers an honest nothing while still answering (a
// ZADD that failed under a Valkey failover while Load succeeded, an evicted
// key) wiped this process's own attempts on every record, so the 800-in-24h
// ceiling never had a count to trip on. Measured 2026-09-07: 21,575 connects
// in 24 hours from one process, zero restarts, against that ceiling.
//
// The merge is keyed on the millisecond, because that is the resolution the
// store round trips through (botstatus.ConnectLog scores by UnixMilli), and
// it keeps the LARGER count on each millisecond rather than one entry per
// millisecond. Two pods can genuinely connect inside the same millisecond --
// the accident this whole budget exists to survive -- and the shared set
// records both under distinct <unixms>-<pod> members, so collapsing a
// millisecond to one attempt would undercount exactly the case that matters
// most.
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

// countByMilli tallies how many attempts landed on each millisecond.
func countByMilli(stamps []time.Time) map[int64]int {
	n := make(map[int64]int, len(stamps))
	for _, at := range stamps {
		n[at.UnixMilli()]++
	}
	return n
}

// persist writes one attempt through to the shared window.
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

// degradedWarnEvery paces the degraded-to-memory notice.
//
// Once per process was what shipped, and it is not enough. The notice is the
// only evidence that the ceiling has stopped being shared, and a pod that
// logged it during boot and then ran for 16 hours left an operator reading a
// 24h window of connect counts with nothing in that window saying the number
// was per process rather than per token. 5 minutes is often enough to appear
// in any window worth reading and rare enough not to bury the gateway's own
// lines: a failing Valkey fails on every note and every record, which on a
// reconnect loop is several calls a second.
const degradedWarnEvery = 5 * time.Minute

// degraded says that the ceiling is now only as good as this process's own
// memory, and keeps saying it for as long as that stays true.
func (b *connectBudget) degraded(err error) {
	if !b.warningDue() {
		return
	}
	b.logger().Warn("discord connect budget is not persisted; counting this process only",
		zap.String("key", ddiscord.BotConnectsKey), zap.Error(err))
}

// warningDue reports whether the degraded notice is due, stamping it when it
// is. Split out so the lock is released before the log write, and so degraded
// itself stays a single decision followed by a single statement.
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

// note records that a socket is being opened right now. It is called on
// every attempt, including the very first, so the ceiling counts what
// Discord counts.
func (b *connectBudget) note() {
	now := b.now()
	b.mu.Lock()
	b.prune(now)
	b.last = now
	b.attempts = append(b.attempts, now)
	b.mu.Unlock()
	// Outside the lock: this is a network round trip, and the only thing it
	// guards is a counter every other reader takes for a snapshot anyway.
	b.persist(now)
}

// record folds one finished session's uptime into the flap detector and
// reports the budget state that follows from it.
func (b *connectBudget) record(up time.Duration) Budget {
	// Read the shared window back before judging it. A second ingress -- the
	// accident this budget exists to survive -- spends the same token's
	// allowance, and a state published from this pod's own attempts alone
	// would report an affordable window while the token was already gone.
	b.reload()
	b.mu.Lock()
	defer b.mu.Unlock()
	// Prune before the verdict, not only in delay(): state() reads Connects
	// and AtCeiling straight off the window, and this is the value that gets
	// published and logged. Without it a pod that spent its allowance
	// yesterday kept reporting AtCeiling long after the window had freed --
	// the reconnect schedule was right and the status key was not.
	b.prune(b.now())
	if up < b.sched.flapUptime {
		b.shortRuns++
	} else {
		b.shortRuns = 0
	}
	b.flapping = b.shortRuns >= b.sched.flapStreak
	return b.state()
}

// delay reports how long to wait before the next connect: the longest of the
// three rules. Zero means the budget is not holding anything back and the
// ordinary reconnect backoff decides.
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

// snapshot is the current state without changing anything.
func (b *connectBudget) snapshot() Budget {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(b.now())
	return b.state()
}

// state assumes the lock is held and the window is pruned.
func (b *connectBudget) state() Budget {
	st := Budget{
		Flapping:  b.flapping,
		Connects:  len(b.attempts),
		AtCeiling: len(b.attempts) >= b.sched.ceiling,
	}
	st.ParkUntil = b.parkUntil(st)
	return st
}

// parkUntil is the deadline behind the two rules that stop the bot rather
// than merely pace it. The ordinary minInterval spacing is deliberately not
// one of them: a 5s gap between identifies is what a healthy reconnect looks
// like, and publishing it as a park would put a countdown on the dashboard
// during every routine gateway roll.
func (b *connectBudget) parkUntil(st Budget) time.Time {
	if st.AtCeiling && len(b.attempts) > 0 {
		return b.attempts[0].Add(b.sched.window)
	}
	if st.Flapping && !b.last.IsZero() {
		return b.last.Add(b.sched.flapWait)
	}
	return time.Time{}
}

// spacing is rule (a), escalated to rule (b) while flapping.
func (b *connectBudget) spacing(now time.Time) time.Duration {
	if b.last.IsZero() {
		return 0
	}
	gap := b.sched.minInterval
	if b.flapping {
		gap = b.sched.flapWait
	}
	return until(b.last.Add(gap), now)
}

// ceilingWait is rule (c): at the ceiling, the next connect waits for the
// oldest attempt to leave the window, which is the earliest instant one more
// connect is affordable. This is the "park like a fatal code" case -- the
// difference is that this park ends on its own.
func (b *connectBudget) ceilingWait(now time.Time) time.Duration {
	if len(b.attempts) < b.sched.ceiling {
		return 0
	}
	return until(b.attempts[0].Add(b.sched.window), now)
}

// prune drops attempts that have aged out of the rolling window.
func (b *connectBudget) prune(now time.Time) {
	cut := now.Add(-b.sched.window)
	i := 0
	for i < len(b.attempts) && !b.attempts[i].After(cut) {
		i++
	}
	b.attempts = b.attempts[i:]
}

// until is the non-negative distance from now to deadline.
func until(deadline, now time.Time) time.Duration {
	if d := deadline.Sub(now); d > 0 {
		return d
	}
	return 0
}
