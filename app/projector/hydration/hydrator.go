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

const operationTimeout = 5 * time.Second

type store interface {
	GetHydrationState(context.Context, uint64) (projection.HydrationState, error)
	SetUserWithTTL(context.Context, uint64, string, bool, bool, time.Duration) error
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

func (h *Hydrator) fill(ctx context.Context, j job, state projection.HydrationState) {
	var (
		userReply     rpcprojection.UserReply
		modulesReply  rpcprojection.ModulesReply
		commandsReply rpcprojection.CommandsReply
		userErr       error
		modulesErr    error
		commandsErr   error
		wg            sync.WaitGroup
	)

	if !state.User {
		wg.Add(1)
		go func() {
			defer wg.Done()
			userReply, userErr = h.fetch.user(ctx, j.userID)
			if userErr == nil && userReply.Error != "" {
				userErr = errors.New(userReply.Error)
			}
		}()
	}

	if !state.Modules {
		if j.seed.ModulesKnown {
			modulesReply.Modules = j.seed.Modules
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()
				modulesReply, modulesErr = h.fetch.modules(ctx, j.userID)
				if modulesErr == nil && modulesReply.Error != "" {
					modulesErr = errors.New(modulesReply.Error)
				}
			}()
		}
	}

	if !state.Commands {
		if j.seed.CommandsKnown {
			commandsReply.Commands = j.seed.Commands
		} else {
			wg.Add(1)
			go func() {
				defer wg.Done()
				commandsReply, commandsErr = h.fetch.commands(ctx, j.userID)
				if commandsErr == nil && commandsReply.Error != "" {
					commandsErr = errors.New(commandsReply.Error)
				}
			}()
		}
	}

	wg.Wait()

	if !state.User {
		if userErr != nil {
			h.logFailure("users", j.userID, userErr)
		} else if err := h.store.SetUserWithTTL(ctx, j.userID, userReply.Status, userReply.IsActive, userReply.Banned, j.ttl); err != nil {
			h.logFailure("users write", j.userID, err)
		}
	}
	if !state.Modules {
		if modulesErr != nil {
			h.logFailure("modules", j.userID, modulesErr)
		} else if err := h.store.SetModulesWithTTL(ctx, j.userID, modulesReply.Modules, j.ttl); err != nil {
			h.logFailure("modules write", j.userID, err)
		}
	}
	if !state.Commands {
		if commandsErr != nil {
			h.logFailure("commands", j.userID, commandsErr)
		} else if err := h.store.SetCommandsWithTTL(ctx, j.userID, commandsReply.Commands, j.ttl); err != nil {
			h.logFailure("commands write", j.userID, err)
		}
	}
}

func (h *Hydrator) logFailure(section string, userID uint64, err error) {
	h.log.Warn("hydration: section failed", zap.String("section", section), zap.Uint64("user_id", userID), zap.Error(err))
}

func request(userID uint64) rpcprojection.Request {
	return rpcprojection.Request{UserID: strconv.FormatUint(userID, 10)}
}
