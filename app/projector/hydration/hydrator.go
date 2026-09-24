// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

const operationTimeout = 8 * time.Second

const hydrationRetryAttempts = 3

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

func (h *Hydrator) EnsureAsync(userID uint64, seed Seed) {
	if userID == 0 || !h.startQuery(userID) {
		return
	}
	go func() {
		defer h.finishQuery(userID)
		h.run(job{userID: userID, ttl: h.queryTTL, seed: seed})
	}()
}

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

type section[T any] struct {
	name     string
	fetch    func(context.Context, uint64) (T, error)
	replyErr func(T) string
	write    func(context.Context, T) error
}

func fillSection[T any](ctx context.Context, h *Hydrator, j job, sec section[T]) {
	reply, err := fetchWithRetry(ctx, h.log, sec.name, j.userID, func(ctx context.Context) (T, error) {
		reply, err := sec.fetch(ctx, j.userID)
		return unwrapReplyErr(reply, err, sec.replyErr)
	})
	if err != nil {
		h.logFailure(sec.name, j.userID, err)
		return
	}
	if err := sec.write(ctx, reply); err != nil {
		h.logFailure(sec.name+" write", j.userID, err)
	}
}

func unwrapReplyErr[T any](reply T, err error, replyErr func(T) string) (T, error) {
	if err != nil {
		return reply, err
	}
	if msg := replyErr(reply); msg != "" {
		return reply, errors.New(msg)
	}
	return reply, nil
}

func (h *Hydrator) fillUser(ctx context.Context, j job) {
	fillSection(ctx, h, j, section[rpcprojection.UserReply]{
		name:     "users",
		fetch:    h.fetch.user,
		replyErr: func(r rpcprojection.UserReply) string { return r.Error },
		write: func(ctx context.Context, r rpcprojection.UserReply) error {
			return h.store.SetUserWithTTL(ctx, j.userID, projection.UserProjection{
				Status:   r.Status,
				IsActive: r.IsActive,
				Banned:   r.Banned,
				Locale:   r.Locale,
			}, j.ttl)
		},
	})
}

func (h *Hydrator) fillModules(ctx context.Context, j job) {
	write := func(ctx context.Context, mods []projection.ModuleView) error {
		return h.store.SetModulesWithTTL(ctx, j.userID, mods, j.ttl)
	}
	if j.seed.ModulesKnown {
		if err := write(ctx, j.seed.Modules); err != nil {
			h.logFailure("modules write", j.userID, err)
		}
		return
	}
	fillSection(ctx, h, j, section[rpcprojection.ModulesReply]{
		name:     "modules",
		fetch:    h.fetch.modules,
		replyErr: func(r rpcprojection.ModulesReply) string { return r.Error },
		write: func(ctx context.Context, r rpcprojection.ModulesReply) error {
			return write(ctx, r.Modules)
		},
	})
}

func (h *Hydrator) fillCommands(ctx context.Context, j job) {
	write := func(ctx context.Context, cmds []projection.CommandView) error {
		return h.store.SetCommandsWithTTL(ctx, j.userID, cmds, j.ttl)
	}
	if j.seed.CommandsKnown {
		if err := write(ctx, j.seed.Commands); err != nil {
			h.logFailure("commands write", j.userID, err)
		}
		return
	}
	fillSection(ctx, h, j, section[rpcprojection.CommandsReply]{
		name:     "commands",
		fetch:    h.fetch.commands,
		replyErr: func(r rpcprojection.CommandsReply) string { return r.Error },
		write: func(ctx context.Context, r rpcprojection.CommandsReply) error {
			return write(ctx, r.Commands)
		},
	})
}

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
