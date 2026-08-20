// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package hydration owns full settings-cache hydration for the projector.
// Dashboard reads use EnsureAsync, which fills only missing sections, while a
// stream-online event uses RefreshAsync to refresh the complete snapshot.
package hydration

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	rpcprojection "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// operationTimeout bounds one full hydration run (fetch + retries + store
// writes for every section). It used to be 5s, sized only for a single
// 1500ms RPC per section. Retrying now needs more: worst case a section
// times out on every attempt, costing
// hydrationRetryAttempts*1500ms + (hydrationRetryAttempts-1)*hydrationRetryBackoff
// = 3*1500ms + 2*150ms = 4800ms. Sections run concurrently, so 4800ms is the
// wall-clock ceiling across all three, not a sum across them. What is left
// (~3.2s) covers the store writes that follow each section's own fetch and
// scheduling jitter under load. 8s was chosen as a round number clear of that
// 4800ms floor; it is still a small, bounded price next to the failure mode
// it replaces, a cold section serving MySQL reads to a broadcaster's first
// live viewers (~3.6ms per row with a warm DB pool, ~25ms with a cold one)
// until a dashboard read or the TTL repairs it.
const operationTimeout = 8 * time.Second

// hydrationRetryAttempts is the total number of tries per section (1 initial
// + 2 retries). RefreshAsync is fire-and-forget with no caller to retry it,
// so a transient NATS blip at go-live previously left that section cold
// until a dashboard read (EnsureAsync) or the TTL lapsed - exactly when the
// broadcaster's first viewers arrive and every miss falls through to MySQL.
// 3 (one attempt plus two retries) is a judgement call, not a measured
// optimum: no soak test of the RPC path has been run. The reasoning is that a
// single retry only survives one dropped message, while go-live blips arrive
// in bursts of redelivery; beyond 3 the remaining failure modes look like
// outages rather than blips, and each extra attempt eats operationTimeout for
// a case retrying cannot fix. Revisit with real numbers if go-live hydration
// failures ever show up in the logs.
const hydrationRetryAttempts = 3

// hydrationRetryBackoff is the pause between retry attempts. 150ms is enough
// for a momentary NATS/RPC hiccup to clear without meaningfully shrinking the
// operationTimeout budget above (2 gaps * 150ms = 300ms of the 4800ms worst
// case). Exponential backoff was considered and rejected: at 3 attempts it
// buys negligible extra tolerance over a fixed gap while making the worst
// case harder to reason about, and go-live self-heal cares about bounded,
// predictable wall clock more than about spacing out load on a healthy bus.
const hydrationRetryBackoff = 150 * time.Millisecond

type store interface {
	GetHydrationState(context.Context, uint64) (projection.HydrationState, error)
	SetUserWithTTL(context.Context, uint64, projection.UserProjection, time.Duration) error
	SetModulesWithTTL(context.Context, uint64, []projection.ModuleView, time.Duration) error
	SetCommandsWithTTL(context.Context, uint64, []projection.CommandView, time.Duration) error
}

type fetchers struct {
	user     func(context.Context, uint64) (rpcprojection.UserReply, error)
	modules  func(context.Context, uint64) (rpcprojection.ModulesReply, error)
	commands func(context.Context, uint64) (rpcprojection.CommandsReply, error)
}

// Seed carries a section already loaded for the foreground request. Known is
// separate from the slice so an intentionally empty result can be reused.
type Seed struct {
	CommandsKnown bool
	Commands      []projection.CommandView
	ModulesKnown  bool
	Modules       []projection.ModuleView
}

func CommandsSeed(commands []projection.CommandView) Seed {
	return Seed{CommandsKnown: true, Commands: append([]projection.CommandView(nil), commands...)}
}

func ModulesSeed(modules []projection.ModuleView) Seed {
	return Seed{ModulesKnown: true, Modules: append([]projection.ModuleView(nil), modules...)}
}

type job struct {
	userID uint64
	force  bool
	ttl    time.Duration
	seed   Seed
}

type userGate struct {
	token chan struct{}
	refs  int
}

// Hydrator bounds full-hydration concurrency and collapses simultaneous query
// fills for the same user. Waiting for the gate happens only in background
// goroutines, never in an RPC handler.
type Hydrator struct {
	store    store
	fetch    fetchers
	queryTTL time.Duration
	liveTTL  time.Duration
	gate     chan struct{}
	log      *zap.Logger

	mu            sync.Mutex
	queryInFlight map[uint64]struct{}
	userGates     map[uint64]*userGate
}

func New(store store, nc *nats.Conn, subjects projection.Subjects, queryTTL, liveTTL time.Duration, concurrency int, log *zap.Logger) *Hydrator {
	if concurrency < 1 {
		concurrency = 1
	}
	if log == nil {
		log = zap.NewNop()
	}

	fetch := fetchers{
		user: func(ctx context.Context, userID uint64) (rpcprojection.UserReply, error) {
			return bus.RequestJSONTimeout[rpcprojection.UserReply](ctx, nc, subjects.Users, request(userID), 1500*time.Millisecond)
		},
		modules: func(ctx context.Context, userID uint64) (rpcprojection.ModulesReply, error) {
			return bus.RequestJSONTimeout[rpcprojection.ModulesReply](ctx, nc, subjects.Modules, request(userID), 1500*time.Millisecond)
		},
		commands: func(ctx context.Context, userID uint64) (rpcprojection.CommandsReply, error) {
			return bus.RequestJSONTimeout[rpcprojection.CommandsReply](ctx, nc, subjects.Commands, request(userID), 1500*time.Millisecond)
		},
	}

	return newHydrator(store, fetch, queryTTL, liveTTL, concurrency, log)
}

func newHydrator(store store, fetch fetchers, queryTTL, liveTTL time.Duration, concurrency int, log *zap.Logger) *Hydrator {
	return &Hydrator{
		store:         store,
		fetch:         fetch,
		queryTTL:      queryTTL,
		liveTTL:       liveTTL,
		gate:          make(chan struct{}, concurrency),
		log:           log,
		queryInFlight: make(map[uint64]struct{}),
		userGates:     make(map[uint64]*userGate),
	}
}

// EnsureAsync checks the full cache on the side and fills only missing
// sections. It returns immediately and collapses concurrent queries per user.
func (h *Hydrator) EnsureAsync(userID uint64, seed Seed) {
	if userID == 0 || !h.startQuery(userID) {
		return
	}
	go func() {
		defer h.finishQuery(userID)
		h.run(job{userID: userID, ttl: h.queryTTL, seed: seed})
	}()
}

// RefreshAsync forces the full snapshot to refresh after a stream-online
// event. It shares the bounded execution path but intentionally does not join
// a query fill: the live refresh and its longer TTL must never be skipped.
func (h *Hydrator) RefreshAsync(userID uint64) {
	if userID == 0 {
		return
	}
	go h.run(job{userID: userID, force: true, ttl: h.liveTTL})
}

func (h *Hydrator) startQuery(userID uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.queryInFlight[userID]; exists {
		return false
	}
	h.queryInFlight[userID] = struct{}{}
	return true
}

func (h *Hydrator) finishQuery(userID uint64) {
	h.mu.Lock()
	delete(h.queryInFlight, userID)
	h.mu.Unlock()
}

func (h *Hydrator) run(j job) {
	releaseUser := h.acquireUser(j.userID)
	defer releaseUser()

	h.gate <- struct{}{}
	defer func() { <-h.gate }()

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()

	state := projection.HydrationState{}
	if !j.force {
		var err error
		state, err = h.store.GetHydrationState(ctx, j.userID)
		if err != nil {
			h.log.Warn("hydration: state check failed", zap.Uint64("user_id", j.userID), zap.Error(err))
			return
		}
		if state.Complete() {
			return
		}
	}

	h.fill(ctx, j, state)
}

// acquireUser serializes query and live hydration for one user. If a live
// refresh arrives during a query fill it runs afterward and wins with the
// freshest full snapshot and longer TTL; a later query sees that complete
// snapshot and exits.
func (h *Hydrator) acquireUser(userID uint64) func() {
	h.mu.Lock()
	g := h.userGates[userID]
	if g == nil {
		g = &userGate{token: make(chan struct{}, 1)}
		h.userGates[userID] = g
	}
	g.refs++
	h.mu.Unlock()

	g.token <- struct{}{}
	return func() {
		<-g.token
		h.mu.Lock()
		g.refs--
		if g.refs == 0 {
			delete(h.userGates, userID)
		}
		h.mu.Unlock()
	}
}

// fill dispatches the three sections concurrently, one goroutine each, and
// waits for all of them. Each section owns its own fetch-retry-then-write
// sequence (see fillUser/fillModules/fillCommands below) instead of the
// fetch-everything-then-write-everything shape this used to have, so a
// section that needs retries never delays writing the sections that already
// succeeded, and a section satisfied from j.seed or already complete in
// state never enters a goroutine at all.
func (h *Hydrator) fill(ctx context.Context, j job, state projection.HydrationState) {
	var wg sync.WaitGroup
	dispatch := func(done bool, section func(context.Context, job)) {
		if done {
			return
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			section(ctx, j)
		}()
	}

	dispatch(state.User, h.fillUser)
	dispatch(state.Modules, h.fillModules)
	dispatch(state.Commands, h.fillCommands)
	wg.Wait()
}

func (h *Hydrator) fillUser(ctx context.Context, j job) {
	reply, err := fetchWithRetry(ctx, h.log, "users", j.userID, func(ctx context.Context) (rpcprojection.UserReply, error) {
		reply, err := h.fetch.user(ctx, j.userID)
		if err == nil && reply.Error != "" {
			err = errors.New(reply.Error)
		}
		return reply, err
	})
	if err != nil {
		h.logFailure("users", j.userID, err)
		return
	}
	if err := h.store.SetUserWithTTL(ctx, j.userID, projection.UserProjection{
		Status:   reply.Status,
		IsActive: reply.IsActive,
		Banned:   reply.Banned,
		Locale:   reply.Locale,
	}, j.ttl); err != nil {
		// Store write failures are not retried here: they are a different
		// failure mode (Valkey, not the go-live NATS/RPC blip this retry
		// exists for) and the section already holds a freshly fetched
		// reply that a later EnsureAsync/RefreshAsync run can re-fetch and
		// write cleanly, so a second write attempt would not add much.
		h.logFailure("users write", j.userID, err)
	}
}

func (h *Hydrator) fillModules(ctx context.Context, j job) {
	if j.seed.ModulesKnown {
		if err := h.store.SetModulesWithTTL(ctx, j.userID, j.seed.Modules, j.ttl); err != nil {
			h.logFailure("modules write", j.userID, err)
		}
		return
	}
	reply, err := fetchWithRetry(ctx, h.log, "modules", j.userID, func(ctx context.Context) (rpcprojection.ModulesReply, error) {
		reply, err := h.fetch.modules(ctx, j.userID)
		if err == nil && reply.Error != "" {
			err = errors.New(reply.Error)
		}
		return reply, err
	})
	if err != nil {
		h.logFailure("modules", j.userID, err)
		return
	}
	if err := h.store.SetModulesWithTTL(ctx, j.userID, reply.Modules, j.ttl); err != nil {
		h.logFailure("modules write", j.userID, err)
	}
}

func (h *Hydrator) fillCommands(ctx context.Context, j job) {
	if j.seed.CommandsKnown {
		if err := h.store.SetCommandsWithTTL(ctx, j.userID, j.seed.Commands, j.ttl); err != nil {
			h.logFailure("commands write", j.userID, err)
		}
		return
	}
	reply, err := fetchWithRetry(ctx, h.log, "commands", j.userID, func(ctx context.Context) (rpcprojection.CommandsReply, error) {
		reply, err := h.fetch.commands(ctx, j.userID)
		if err == nil && reply.Error != "" {
			err = errors.New(reply.Error)
		}
		return reply, err
	})
	if err != nil {
		h.logFailure("commands", j.userID, err)
		return
	}
	if err := h.store.SetCommandsWithTTL(ctx, j.userID, reply.Commands, j.ttl); err != nil {
		h.logFailure("commands write", j.userID, err)
	}
}

// fetchWithRetry runs fetch up to hydrationRetryAttempts times, pausing
// hydrationRetryBackoff between tries, and returns the last result once it
// succeeds or the attempts are exhausted. It is a free function rather than
// a Hydrator method because Go does not allow a method to carry its own type
// parameter, and each section's reply type differs (UserReply, ModulesReply,
// CommandsReply). The wait between attempts happens here, inside the
// goroutine fill() already spawned per section - never synchronously in an
// RPC handler - matching the package invariant that waiting on the gate or
// on retries only ever happens in background goroutines.
func fetchWithRetry[T any](ctx context.Context, log *zap.Logger, section string, userID uint64, fetch func(context.Context) (T, error)) (T, error) {
	var reply T
	var err error
	for attempt := 1; attempt <= hydrationRetryAttempts; attempt++ {
		reply, err = fetch(ctx)
		if err == nil {
			return reply, nil
		}
		if attempt == hydrationRetryAttempts {
			break
		}
		log.Debug("hydration: section retrying", zap.String("section", section), zap.Uint64("user_id", userID), zap.Int("attempt", attempt), zap.Error(err))
		if waitErr := sleepOrCancel(ctx, hydrationRetryBackoff); waitErr != nil {
			return reply, err
		}
	}
	return reply, err
}

// sleepOrCancel waits for d, or returns early if ctx is done first, so a
// retry backoff never overruns operationTimeout.
func sleepOrCancel(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

func (h *Hydrator) logFailure(section string, userID uint64, err error) {
	h.log.Warn("hydration: section failed", zap.String("section", section), zap.Uint64("user_id", userID), zap.Error(err))
}

func request(userID uint64) rpcprojection.Request {
	return rpcprojection.Request{UserID: strconv.FormatUint(userID, 10)}
}
