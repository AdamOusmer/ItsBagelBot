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

// Bump records one counter delta. command keys the bucket of a command /
// viewer+command bump ("" everywhere else). viewer carries the chatter's
// display identity when the source knew it. Broadcaster 0 is the reserved bot
// namespace and only carries bot-scope bumps.
func (r *LoyaltyReporter) Bump(broadcasterID uint64, name, scope string, viewer Viewer, command string, delta int64) {
	if name == "" || delta == 0 || (broadcasterID == 0) != (scope == data.CounterScopeBot) {
		return
	}
	key := counterAgg{broadcasterID: broadcasterID, name: name, scope: scope, viewerID: viewer.ID, command: command}

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
