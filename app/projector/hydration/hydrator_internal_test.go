// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
	setUser     func(context.Context, uint64, projection.UserProjection, time.Duration) error
	setModules  func(context.Context, uint64, []projection.ModuleView, time.Duration) error
	setCommands func(context.Context, uint64, []projection.CommandView,
		time.Duration) error
}

func (s *fakeStore) GetHydrationState(ctx context.Context, id uint64) (projection.HydrationState, error) {
	return s.getState(ctx, id)
}

func (s *fakeStore) SetUserWithTTL(ctx context.Context, id uint64, u projection.UserProjection, ttl time.Duration) error {
	return s.setUser(ctx, id, u, ttl)
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

func TestEnsureAsyncRetriesTransientFetchFailureThenSucceeds(t *testing.T) {
	writes := make(chan write, 3)
	store := noOpStore()
	store.getState = func(context.Context, uint64) (projection.HydrationState, error) {
		return projection.HydrationState{}, nil
	}
	store.setUser = func(_ context.Context, _ uint64, _ projection.UserProjection, ttl time.Duration) error {
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

	var moduleFetches atomic.Int32
	fetch := noOpFetchers()
	fetch.modules = func(context.Context, uint64) (rpcprojection.ModulesReply, error) {
		if moduleFetches.Add(1) < int32(hydrationRetryAttempts) {
			return rpcprojection.ModulesReply{}, errors.New("transient nats blip")
		}
		return rpcprojection.ModulesReply{Modules: []projection.ModuleView{{Name: "greet"}}}, nil
	}

	h := newHydrator(store, fetch, 2*time.Hour, 24*time.Hour, 1, zap.NewNop())
	h.EnsureAsync(42, Seed{})

	got := collectWrites(t, writes, 3)
	var modulesWrite write
	for _, w := range got {
		if w.section == "modules" {
			modulesWrite = w
		}
	}
	require.Equal(t, write{section: "modules", ttl: 2 * time.Hour, count: 1}, modulesWrite)
	require.Equal(t, int32(hydrationRetryAttempts), moduleFetches.Load(),
		"must succeed on the last allowed attempt, proving retry recovered it rather than a single lucky call")
}

func TestFillUserKeepsCommandsPageHidden(t *testing.T) {
	var written projection.UserProjection
	store := noOpStore()
	store.setUser = func(_ context.Context, _ uint64, u projection.UserProjection, _ time.Duration) error {
		written = u
		return nil
	}
	fetch := noOpFetchers()
	fetch.user = func(context.Context, uint64) (rpcprojection.UserReply, error) {
		return rpcprojection.UserReply{StateRevision: 3, Status: "paid", IsActive: true, CommandsPageHidden: true}, nil
	}

	h := newHydrator(store, fetch, 2*time.Hour, 24*time.Hour, 1, zap.NewNop())
	h.fillUser(context.Background(), job{userID: 42, ttl: 2 * time.Hour})

	require.True(t, written.CommandsPageHidden, "cold hydration must not reset a hidden commands page to visible")
	require.Equal(t, int64(3), written.StateRevision)
}

func noOpStore() *fakeStore {
	return &fakeStore{
		getState: func(context.Context, uint64) (projection.HydrationState, error) {
			return projection.HydrationState{}, nil
		},
		setUser: func(context.Context, uint64, projection.UserProjection, time.Duration) error { return nil },
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
