package hydration

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	rpcprojection "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeStore struct {
	getState    func(context.Context, uint64) (projection.HydrationState, error)
	setUser     func(context.Context, uint64, string, bool, bool, string, time.Duration) error
	setModules  func(context.Context, uint64, []projection.ModuleView, time.Duration) error
	setCommands func(context.Context, uint64, []projection.CommandView,
		time.Duration) error
}

func (s *fakeStore) GetHydrationState(ctx context.Context, id uint64) (projection.HydrationState, error) {
	return s.getState(ctx, id)
}

func (s *fakeStore) SetUserWithTTL(ctx context.Context, id uint64, status string, active, banned bool, locale string, ttl time.Duration) error {
	return s.setUser(ctx, id, status, active, banned, locale, ttl)
}

func (s *fakeStore) SetModulesWithTTL(ctx context.Context, id uint64, modules []projection.ModuleView, ttl time.Duration) error {
	return s.setModules(ctx, id, modules, ttl)
}

func (s *fakeStore) SetCommandsWithTTL(ctx context.Context, id uint64, commands []projection.CommandView, ttl time.Duration) error {
	return s.setCommands(ctx, id, commands, ttl)
}

type write struct {
	section string
	ttl     time.Duration
	count   int
}

func TestEnsureAsyncReturnsBeforeHydrationCheckCompletes(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	store := noOpStore()
	store.getState = func(context.Context, uint64) (projection.HydrationState, error) {
		close(started)
		<-release
		return projection.HydrationState{User: true, Modules: true, Commands: true}, nil
	}
	h := newHydrator(store, noOpFetchers(), 2*time.Hour, 24*time.Hour, 1, zap.NewNop())

	returned := make(chan struct{})
	go func() {
		h.EnsureAsync(42, Seed{})
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("EnsureAsync blocked the caller")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background hydration did not start")
	}
	close(release)
}

func TestEnsureAsyncHydratesMissingSectionsWithQueryTTLAndReusesSeed(t *testing.T) {
	writes := make(chan write, 3)
	store := noOpStore()
	store.getState = func(context.Context, uint64) (projection.HydrationState, error) {
		return projection.HydrationState{}, nil
	}
	store.setUser = func(_ context.Context, _ uint64, _ string, _ bool, _ bool, _ string, ttl time.Duration) error {
		writes <- write{section: "user", ttl: ttl}
		return nil
	}
	store.setModules = func(_ context.Context, _ uint64, modules []projection.ModuleView, ttl time.Duration) error {
		writes <- write{section: "modules", ttl: ttl, count: len(modules)}
		return nil
	}
	store.setCommands = func(_ context.Context, _ uint64, commands []projection.CommandView, ttl time.Duration) error {
		writes <- write{section: "commands", ttl: ttl, count: len(commands)}
		return nil
	}

	var commandFetches atomic.Int32
	fetch := noOpFetchers()
	fetch.user = func(context.Context, uint64) (rpcprojection.UserReply, error) {
		return rpcprojection.UserReply{Status: "paid", IsActive: true}, nil
	}
	fetch.modules = func(context.Context, uint64) (rpcprojection.ModulesReply, error) {
		return rpcprojection.ModulesReply{Modules: []projection.ModuleView{{Name: "greet"}}}, nil
	}
	fetch.commands = func(context.Context, uint64) (rpcprojection.CommandsReply, error) {
		commandFetches.Add(1)
		return rpcprojection.CommandsReply{}, nil
	}

	h := newHydrator(store, fetch, 2*time.Hour, 24*time.Hour, 2, zap.NewNop())
	h.EnsureAsync(42, CommandsSeed(nil))

	got := collectWrites(t, writes, 3)
	require.Equal(t, int32(0), commandFetches.Load(), "empty foreground result must still count as a reusable seed")
	require.Equal(t, write{section: "user", ttl: 2 * time.Hour}, got[0])
	require.Equal(t, write{section: "modules", ttl: 2 * time.Hour, count: 1}, got[1])
	require.Equal(t, write{section: "commands", ttl: 2 * time.Hour}, got[2])
}

func TestEnsureAsyncSkipsFullyHydratedCache(t *testing.T) {
	checked := make(chan struct{})
	called := make(chan string, 1)
	store := noOpStore()
	store.getState = func(context.Context, uint64) (projection.HydrationState, error) {
		close(checked)
		return projection.HydrationState{User: true, Modules: true, Commands: true}, nil
	}
	fetch := noOpFetchers()
	fetch.user = func(context.Context, uint64) (rpcprojection.UserReply, error) {
		called <- "user"
		return rpcprojection.UserReply{}, nil
	}
	fetch.modules = func(context.Context, uint64) (rpcprojection.ModulesReply, error) {
		called <- "modules"
		return rpcprojection.ModulesReply{}, nil
	}
	fetch.commands = func(context.Context, uint64) (rpcprojection.CommandsReply, error) {
		called <- "commands"
		return rpcprojection.CommandsReply{}, nil
	}

	h := newHydrator(store, fetch, 2*time.Hour, 24*time.Hour, 1, zap.NewNop())
	h.EnsureAsync(42, Seed{})
	waitFor(t, checked)
	select {
	case section := <-called:
		t.Fatalf("complete cache unexpectedly fetched %s", section)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestEnsureAsyncCollapsesConcurrentQueriesPerUser(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var checks atomic.Int32
	store := noOpStore()
	store.getState = func(context.Context, uint64) (projection.HydrationState, error) {
		if checks.Add(1) == 1 {
			close(started)
		}
		<-release
		return projection.HydrationState{User: true, Modules: true, Commands: true}, nil
	}
	h := newHydrator(store, noOpFetchers(), 2*time.Hour, 24*time.Hour, 2, zap.NewNop())

	h.EnsureAsync(42, Seed{})
	waitFor(t, started)
	for range 20 {
		h.EnsureAsync(42, Seed{})
	}
	close(release)
	require.Eventually(t, func() bool { return checks.Load() == 1 }, time.Second, 10*time.Millisecond)
}

func TestHydrationConcurrencyIsBounded(t *testing.T) {
	entered := make(chan uint64, 2)
	release := make(chan struct{})
	store := noOpStore()
	store.getState = func(_ context.Context, userID uint64) (projection.HydrationState, error) {
		entered <- userID
		<-release
		return projection.HydrationState{User: true, Modules: true, Commands: true}, nil
	}
	h := newHydrator(store, noOpFetchers(), 2*time.Hour, 24*time.Hour, 1, zap.NewNop())

	h.EnsureAsync(41, Seed{})
	h.EnsureAsync(42, Seed{})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("first hydration did not enter")
	}
	select {
	case id := <-entered:
		t.Fatalf("second hydration %d exceeded concurrency limit", id)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("queued hydration did not enter after capacity was released")
	}
}

func TestRefreshAsyncForcesFullHydrationWithLiveTTL(t *testing.T) {
	writes := make(chan write, 3)
	store := noOpStore()
	store.getState = func(context.Context, uint64) (projection.HydrationState, error) {
		t.Fatal("forced live refresh must not use the completeness shortcut")
		return projection.HydrationState{}, nil
	}
	store.setUser = func(_ context.Context, _ uint64, _ string, _ bool, _ bool, _ string, ttl time.Duration) error {
		writes <- write{section: "user", ttl: ttl}
		return nil
	}
	store.setModules = func(_ context.Context, _ uint64, values []projection.ModuleView, ttl time.Duration) error {
		writes <- write{section: "modules", ttl: ttl, count: len(values)}
		return nil
	}
	store.setCommands = func(_ context.Context, _ uint64, values []projection.CommandView, ttl time.Duration) error {
		writes <- write{section: "commands", ttl: ttl, count: len(values)}
		return nil
	}

	fetch := noOpFetchers()
	h := newHydrator(store, fetch, 2*time.Hour, 24*time.Hour, 1, zap.NewNop())
	h.RefreshAsync(42)

	for _, item := range collectWrites(t, writes, 3) {
		require.Equal(t, 24*time.Hour, item.ttl)
	}
}

func TestEnsureAsyncLeavesFailedSectionUnprojectedForRetry(t *testing.T) {
	writes := make(chan write, 3)
	store := noOpStore()
	store.getState = func(context.Context, uint64) (projection.HydrationState, error) {
		return projection.HydrationState{}, nil
	}
	store.setUser = func(_ context.Context, _ uint64, _ string, _ bool, _ bool, _ string, ttl time.Duration) error {
		writes <- write{section: "user", ttl: ttl}
		return nil
	}
	store.setModules = func(_ context.Context, _ uint64, _ []projection.ModuleView, ttl time.Duration) error {
		writes <- write{section: "modules", ttl: ttl}
		return nil
	}
	store.setCommands = func(_ context.Context, _ uint64, _ []projection.CommandView, ttl time.Duration) error {
		writes <- write{section: "commands", ttl: ttl}
		return nil
	}
	fetch := noOpFetchers()
	fetch.commands = func(context.Context, uint64) (rpcprojection.CommandsReply, error) {
		return rpcprojection.CommandsReply{}, errors.New("commands unavailable")
	}

	h := newHydrator(store, fetch, 2*time.Hour, 24*time.Hour, 1, zap.NewNop())
	h.EnsureAsync(42, Seed{})

	got := collectWrites(t, writes, 2)
	require.Equal(t, "user", got[0].section)
	require.Equal(t, "modules", got[1].section)
	select {
	case extra := <-writes:
		t.Fatalf("failed section was written: %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func noOpStore() *fakeStore {
	return &fakeStore{
		getState: func(context.Context, uint64) (projection.HydrationState, error) {
			return projection.HydrationState{}, nil
		},
		setUser: func(context.Context, uint64, string, bool, bool, string, time.Duration) error { return nil },
		setModules: func(context.Context, uint64, []projection.ModuleView, time.Duration) error {
			return nil
		},
		setCommands: func(context.Context, uint64, []projection.CommandView, time.Duration) error {
			return nil
		},
	}
}

func noOpFetchers() fetchers {
	return fetchers{
		user: func(context.Context, uint64) (rpcprojection.UserReply, error) {
			return rpcprojection.UserReply{}, nil
		},
		modules: func(context.Context, uint64) (rpcprojection.ModulesReply, error) {
			return rpcprojection.ModulesReply{}, nil
		},
		commands: func(context.Context, uint64) (rpcprojection.CommandsReply, error) {
			return rpcprojection.CommandsReply{}, nil
		},
	}
}

func collectWrites(t *testing.T, ch <-chan write, count int) []write {
	t.Helper()
	out := make([]write, 0, count)
	for range count {
		select {
		case item := <-ch:
			out = append(out, item)
		case <-time.After(time.Second):
			t.Fatalf("timed out after %d/%d writes", len(out), count)
		}
	}
	return out
}

func waitFor(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for background operation")
	}
}
