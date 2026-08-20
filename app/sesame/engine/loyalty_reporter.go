// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/bus"

	"go.uber.org/zap"
)

const (
	// loyaltyFlushInterval bounds the bus rate: a gift bomb, a cheer train or
	// a watch tick over a big channel costs one summed event per broadcaster
	// per window instead of one per accrual.
	loyaltyFlushInterval = 5 * time.Second

	// loyaltyMaxKeys triggers an early flush when either pending map grows
	// past it. Entries are never dropped (unlike the use reporter's newest-key
	// drop): a watch tick legitimately adds thousands of keys in one call, and
	// dropping them would silently unfairly skip viewers. The maps stay
	// bounded because the flush drains them.
	loyaltyMaxKeys = 8192

	// loyaltyChunk bounds one published event's entry list, keeping a big
	// channel's watch tick far under the broker's payload ceiling.
	loyaltyChunk = 1000
)

type earnKey struct {
	broadcasterID uint64
	viewerID      uint64
}

type earnAgg struct {
	points       int64
	watchSeconds uint64
	login        string
	name         string
}

type counterAgg struct {
	broadcasterID uint64
	name          string
	scope         string
	viewerID      uint64
	command       string
}

// bumpAgg is one counter bucket's summed delta plus the freshest viewer
// identity seen this window (empty means "no bump carried it; the service
// keeps whatever it stored").
type bumpAgg struct {
	delta int64
	login string
	name  string
}

// LoyaltyReporter aggregates point accruals and counter bumps per flush window
// and publishes summed data.loyalty.* events, chunked per broadcaster. It is
// the worker-side rate limiter for the loyalty pipeline, the same role the
// useReporter plays for command uses.
type LoyaltyReporter struct {
	pub  bus.Publisher
	log  *zap.Logger
	done chan struct{}
	wake chan struct{}

	mu    sync.Mutex
	earn  map[earnKey]*earnAgg
	bumps map[counterAgg]*bumpAgg
}

func NewLoyaltyReporter(pub bus.Publisher, log *zap.Logger) *LoyaltyReporter {
	r := &LoyaltyReporter{
		pub:   pub,
		log:   log,
		done:  make(chan struct{}),
		wake:  make(chan struct{}, 1),
		earn:  map[earnKey]*earnAgg{},
		bumps: map[counterAgg]*bumpAgg{},
	}
	go func() {
		ticker := time.NewTicker(loyaltyFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				r.flush(context.Background())
			case <-r.wake:
				r.flush(context.Background())
			case <-r.done:
				return
			}
		}
	}()
	return r
}

// Earn records one viewer's accrual. Never blocks the hot path: it takes a
// short mutex and, past the key cap, nudges the flusher instead of publishing
// inline.
func (r *LoyaltyReporter) Earn(broadcasterID, viewerID uint64, login, name string, points int64, watchSeconds uint64) {
	if broadcasterID == 0 || viewerID == 0 || (points == 0 && watchSeconds == 0) {
		return
	}
	key := earnKey{broadcasterID: broadcasterID, viewerID: viewerID}

	r.mu.Lock()
	agg := r.earn[key]
	if agg == nil {
		agg = &earnAgg{}
		r.earn[key] = agg
	}
	agg.points += points
	agg.watchSeconds += watchSeconds
	if login != "" {
		agg.login = login
	}
	if name != "" {
		agg.name = name
	}
	overflow := len(r.earn) >= loyaltyMaxKeys
	r.mu.Unlock()

	if overflow {
		r.nudge()
	}
}

// CounterBumpTarget names the row one bump lands on: whose counter it is, which
// counter, its scope, and — for the entry-scoped kinds — which bucket inside
// it. The fields always travel together (they are exactly the accumulator's key
// plus the viewer identity that rides along with it), so they travel as one
// value rather than as five positional arguments at every call site. The
// sibling CounterTarget in dedup.go is the read side of the same idea; a bump
// additionally carries the scope and the viewer's display identity, which a
// peek has no use for.
//
// A zero BroadcasterID is the reserved bot namespace, which carries bot-scope
// counters and nothing else; the pairing is enforced in Bump.
type CounterBumpTarget struct {
	BroadcasterID uint64
	Name          string
	Scope         string
	// Viewer carries the chatter's display identity when the source knew it,
	// and their id keys the bucket of a viewer / viewer+command counter.
	Viewer Viewer
	// Command keys the bucket of a command / viewer+command counter; empty
	// everywhere else.
	Command string
}

// ChannelBump targets a plain per-channel counter: no viewer, no command
// bucket, just a broadcaster's own row.
func ChannelBump(broadcasterID uint64, name string) CounterBumpTarget {
	return CounterBumpTarget{BroadcasterID: broadcasterID, Name: name, Scope: data.CounterScopeChannel}
}

// BotBump targets a fleet-wide counter: the reserved broadcaster-0 namespace,
// bot scope.
func BotBump(name string) CounterBumpTarget {
	return CounterBumpTarget{Name: name, Scope: data.CounterScopeBot}
}

// BumpBot records one bot-scope counter delta under the reserved broadcaster-0
// namespace: the narrow entry the pipeline's stats flusher uses, so callers
// that only ever bump bot-wide counters name nothing but the counter.
func (r *LoyaltyReporter) BumpBot(name string, delta int64) {
	r.Bump(BotBump(name), delta)
}

// BumpChannel records one channel-scope counter delta for a broadcaster: the
// pipeline's per-channel traffic split, which needs neither a viewer nor a
// command bucket.
func (r *LoyaltyReporter) BumpChannel(broadcasterID uint64, name string, delta int64) {
	r.Bump(ChannelBump(broadcasterID, name), delta)
}

// Bump records one counter delta against a target. A nameless counter, a zero
// delta, or a broadcaster/scope pairing that contradicts the bot namespace is
// dropped rather than aggregated: the loyalty service rejects all three anyway,
// so the flush should not carry them.
func (r *LoyaltyReporter) Bump(target CounterBumpTarget, delta int64) {
	name, viewer := target.Name, target.Viewer
	if name == "" || delta == 0 || (target.BroadcasterID == 0) != (target.Scope == data.CounterScopeBot) {
		return
	}
	key := counterAgg{
		broadcasterID: target.BroadcasterID,
		name:          name,
		scope:         target.Scope,
		viewerID:      viewer.ID,
		command:       target.Command,
	}

	r.mu.Lock()
	agg := r.bumps[key]
	if agg == nil {
		agg = &bumpAgg{}
		r.bumps[key] = agg
	}
	agg.delta += delta
	if viewer.Login != "" {
		agg.login = viewer.Login
	}
	if viewer.Name != "" {
		agg.name = viewer.Name
	}
	overflow := len(r.bumps) >= loyaltyMaxKeys
	r.mu.Unlock()

	if overflow {
		r.nudge()
	}
}

// nudge asks the flusher goroutine for an early pass; a full channel means one
// is already queued.
func (r *LoyaltyReporter) nudge() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// flush drains both maps and publishes summed events, grouped per broadcaster
// and chunked.
func (r *LoyaltyReporter) flush(ctx context.Context) {
	r.mu.Lock()
	earn, bumps := r.earn, r.bumps
	if len(earn) > 0 {
		r.earn = map[earnKey]*earnAgg{}
	}
	if len(bumps) > 0 {
		r.bumps = map[counterAgg]*bumpAgg{}
	}
	r.mu.Unlock()

	r.publishEarned(ctx, earn)
	r.publishBumps(ctx, bumps)
}

func (r *LoyaltyReporter) publishEarned(ctx context.Context, earn map[earnKey]*earnAgg) {
	perUser := map[uint64][]data.LoyaltyEarnEntry{}
	for key, agg := range earn {
		perUser[key.broadcasterID] = append(perUser[key.broadcasterID], data.LoyaltyEarnEntry{
			ViewerID:     key.viewerID,
			ViewerLogin:  agg.login,
			ViewerName:   agg.name,
			Points:       agg.points,
			WatchSeconds: agg.watchSeconds,
		})
	}
	publishPerUser(ctx, r, perUser, data.SubjectLoyaltyEarned, func(userID uint64, chunk []data.LoyaltyEarnEntry) any {
		return data.LoyaltyEarnedDTO{UserID: userID, Entries: chunk}
	})
}

func (r *LoyaltyReporter) publishBumps(ctx context.Context, bumps map[counterAgg]*bumpAgg) {
	perUser := map[uint64][]data.CounterBumpEntry{}
	for key, agg := range bumps {
		perUser[key.broadcasterID] = append(perUser[key.broadcasterID], data.CounterBumpEntry{
			Name:        key.name,
			Scope:       key.scope,
			ViewerID:    key.viewerID,
			ViewerLogin: agg.login,
			ViewerName:  agg.name,
			Command:     key.command,
			Delta:       agg.delta,
		})
	}
	publishPerUser(ctx, r, perUser, data.SubjectLoyaltyCounters, func(userID uint64, chunk []data.CounterBumpEntry) any {
		return data.CounterBumpedDTO{UserID: userID, Bumps: chunk}
	})
}

// publishPerUser publishes one window's aggregates: per broadcaster, chunked
// so a big channel's watch tick never approaches the broker payload ceiling.
// A failed publish is logged and dropped (loss-tolerant deltas).
func publishPerUser[E any](ctx context.Context, r *LoyaltyReporter, perUser map[uint64][]E, subject string, wrap func(uint64, []E) any) {
	for userID, entries := range perUser {
		for start := 0; start < len(entries); start += loyaltyChunk {
			chunk := entries[start:min(start+loyaltyChunk, len(entries))]
			if err := bus.PublishJSON(ctx, r.pub, subject, wrap(userID, chunk)); err != nil {
				r.log.Debug("failed to publish loyalty window",
					zap.String("subject", subject),
					zap.Uint64("broadcaster_id", userID),
					zap.Int("entries", len(chunk)),
					zap.Error(err),
				)
			}
		}
	}
}

// Close stops the ticker and flushes what is pending.
func (r *LoyaltyReporter) Close() {
	close(r.done)
	r.flush(context.Background())
}
