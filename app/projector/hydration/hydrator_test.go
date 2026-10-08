// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package hydration_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/app/projector/hydration"
	rpcprojection "ItsBagelBot/internal/domain/rpc/projection"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const (
	queryTTL = 2 * time.Hour
	liveTTL  = 24 * time.Hour
)

var complete = projection.HydrationState{User: true, Modules: true, Commands: true}

type write struct {
	section string
	ttl     time.Duration
	count   int
}

type memStore struct {
	state  projection.HydrationState
	onScan func(userID uint64)
	scans  atomic.Int32
	writes chan write
}

func newMemStore(state projection.HydrationState) *memStore {
	return &memStore{state: state, writes: make(chan write, 8)}
}

func (s *memStore) GetHydrationState(_ context.Context, userID uint64) (projection.HydrationState, error) {
	s.scans.Add(1)
	if s.onScan != nil {
		s.onScan(userID)
	}
	return s.state, nil
}

func (s *memStore) SetUserWithTTL(_ context.Context, _ uint64, _ projection.UserProjection, ttl time.Duration) error {
	s.writes <- write{section: "user", ttl: ttl}
	return nil
}

func (s *memStore) SetModulesWithTTL(_ context.Context, _ uint64, modules []projection.ModuleView, ttl time.Duration) error {
	s.writes <- write{section: "modules", ttl: ttl, count: len(modules)}
	return nil
}

func (s *memStore) SetCommandsWithTTL(_ context.Context, _ uint64, commands []projection.CommandView, ttl time.Duration) error {
	s.writes <- write{section: "commands", ttl: ttl, count: len(commands)}
	return nil
}

func (s *memStore) collect(t *testing.T, count int) map[string]write {
	t.Helper()
	got := make(map[string]write, count)
	for range count {
		select {
		case w := <-s.writes:
			got[w.section] = w
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out after %d/%d writes", len(got), count)
		}
	}
	return got
}

func (s *memStore) neverWrites(t *testing.T, window time.Duration) {
	t.Helper()
	select {
	case w := <-s.writes:
		t.Fatalf("unexpected write %+v", w)
	case <-time.After(window):
	}
}

type upstream struct {
	subjects projection.Subjects
	hits     map[string]*atomic.Int32
}

type replies map[string]func(attempt int) any

func newUpstream(t *testing.T, override replies) (*nats.Conn, upstream) {
	t.Helper()
	nc := testnats.Connect(t)
	up := upstream{
		subjects: projection.Subjects{Users: "test.users", Modules: "test.modules", Commands: "test.commands"},
		hits:     map[string]*atomic.Int32{"users": {}, "modules": {}, "commands": {}},
	}
	defaults := replies{
		"users":    func(int) any { return rpcprojection.UserReply{Status: "paid", IsActive: true} },
		"modules":  func(int) any { return rpcprojection.ModulesReply{Modules: []projection.ModuleView{{Name: "greet"}}} },
		"commands": func(int) any { return rpcprojection.CommandsReply{} },
	}
	for section, subject := range map[string]string{"users": up.subjects.Users, "modules": up.subjects.Modules, "commands": up.subjects.Commands} {
		reply := defaults[section]
		if custom, ok := override[section]; ok {
			reply = custom
		}
		_, err := nc.Subscribe(subject, func(msg *nats.Msg) {
			body, _ := codec.Marshal(reply(int(up.hits[section].Add(1))))
			_ = msg.Respond(body)
		})
		require.NoError(t, err)
	}
	require.NoError(t, nc.Flush())
	return nc, up
}

func (u upstream) fetched() map[string]int32 {
	return map[string]int32{"users": u.hits["users"].Load(), "modules": u.hits["modules"].Load(), "commands": u.hits["commands"].Load()}
}

func newHydrator(nc *nats.Conn, up upstream, store *memStore, concurrency int) *hydration.Hydrator {
	return hydration.New(store, nc, up.subjects, queryTTL, liveTTL, concurrency, zap.NewNop())
}

func signal() (func(), <-chan struct{}) {
	var once atomic.Bool
	ch := make(chan struct{})
	return func() {
		if once.CompareAndSwap(false, true) {
			close(ch)
		}
	}, ch
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestEnsureAsyncReturnsBeforeHydrationCheckCompletes(t *testing.T) {
	nc, up := newUpstream(t, nil)
	store := newMemStore(complete)
	started, startedCh := signal()
	release := make(chan struct{})
	store.onScan = func(uint64) { started(); <-release }
	h := newHydrator(nc, up, store, 1)

	returned := make(chan struct{})
	go func() {
		h.EnsureAsync(42, hydration.Seed{})
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("EnsureAsync blocked the caller")
	}
	waitFor(t, startedCh, "background hydration")
	close(release)
}

func TestEnsureAsyncHydratesMissingSectionsWithQueryTTLAndReusesSeed(t *testing.T) {
	for _, tc := range []struct {
		name        string
		seed        hydration.Seed
		wantWrites  map[string]write
		wantFetched map[string]int32
	}{
		{
			name: "an empty foreground commands result still counts as a reusable seed",
			seed: hydration.CommandsSeed(nil),
			wantWrites: map[string]write{
				"user":     {section: "user", ttl: queryTTL},
				"modules":  {section: "modules", ttl: queryTTL, count: 1},
				"commands": {section: "commands", ttl: queryTTL},
			},
			wantFetched: map[string]int32{"users": 1, "modules": 1, "commands": 0},
		},
		{
			name: "a known modules snapshot is written without a modules fetch",
			seed: hydration.ModulesSeed([]projection.ModuleView{{Name: "queue"}, {Name: "greet"}}),
			wantWrites: map[string]write{
				"user":     {section: "user", ttl: queryTTL},
				"modules":  {section: "modules", ttl: queryTTL, count: 2},
				"commands": {section: "commands", ttl: queryTTL},
			},
			wantFetched: map[string]int32{"users": 1, "modules": 0, "commands": 1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nc, up := newUpstream(t, nil)
			store := newMemStore(projection.HydrationState{})

			newHydrator(nc, up, store, 2).EnsureAsync(42, tc.seed)

			assert.Equal(t, tc.wantWrites, store.collect(t, 3))
			assert.Equal(t, tc.wantFetched, up.fetched())
		})
	}
}

func TestEnsureAsyncSkipsFullyHydratedCache(t *testing.T) {
	nc, up := newUpstream(t, nil)
	store := newMemStore(complete)

	newHydrator(nc, up, store, 1).EnsureAsync(42, hydration.Seed{})

	require.Eventually(t, func() bool { return store.scans.Load() == 1 }, time.Second, 5*time.Millisecond)
	store.neverWrites(t, 100*time.Millisecond)
	assert.Equal(t, map[string]int32{"users": 0, "modules": 0, "commands": 0}, up.fetched(), "a complete cache fetches nothing")
}

func TestEnsureAsyncCollapsesConcurrentQueriesPerUser(t *testing.T) {
	nc, up := newUpstream(t, nil)
	store := newMemStore(complete)
	started, startedCh := signal()
	release := make(chan struct{})
	store.onScan = func(uint64) { started(); <-release }
	h := newHydrator(nc, up, store, 2)

	h.EnsureAsync(42, hydration.Seed{})
	waitFor(t, startedCh, "first scan")
	for range 20 {
		h.EnsureAsync(42, hydration.Seed{})
	}
	close(release)

	require.Never(t, func() bool { return store.scans.Load() > 1 }, 200*time.Millisecond, 10*time.Millisecond)
}

func TestHydrationConcurrencyIsBounded(t *testing.T) {
	nc, up := newUpstream(t, nil)
	store := newMemStore(complete)
	entered := make(chan uint64, 2)
	release := make(chan struct{})
	store.onScan = func(userID uint64) { entered <- userID; <-release }
	h := newHydrator(nc, up, store, 1)

	h.EnsureAsync(41, hydration.Seed{})
	h.EnsureAsync(42, hydration.Seed{})
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
	nc, up := newUpstream(t, nil)
	store := newMemStore(complete)

	newHydrator(nc, up, store, 1).RefreshAsync(42)

	assert.Equal(t, map[string]write{
		"user":     {section: "user", ttl: liveTTL},
		"modules":  {section: "modules", ttl: liveTTL, count: 1},
		"commands": {section: "commands", ttl: liveTTL},
	}, store.collect(t, 3))
	assert.Zero(t, store.scans.Load(), "a forced live refresh must not use the completeness shortcut")
}

func TestEnsureAsyncLeavesFailedSectionUnprojectedForRetry(t *testing.T) {
	const attempts = 3
	nc, up := newUpstream(t, replies{
		"commands": func(int) any { return map[string]string{"error": "commands unavailable"} },
	})
	store := newMemStore(projection.HydrationState{})

	newHydrator(nc, up, store, 1).EnsureAsync(42, hydration.Seed{})

	assert.Contains(t, store.collect(t, 2), "user")
	require.Eventually(t, func() bool { return up.hits["commands"].Load() == attempts }, 3*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return up.hits["commands"].Load() > attempts }, 500*time.Millisecond, 10*time.Millisecond,
		"an always-failing section must be retried a bounded number of times")
	store.neverWrites(t, 10*time.Millisecond)
}

func TestMissingAccountIsFinalAndQuiet(t *testing.T) {
	const attempts = 3
	for _, tc := range []struct {
		name        string
		code        string
		wantFetches int32
		wantWarns   int
	}{
		{"a not_found account is fetched once and not warned about", "not_found", 1, 0},
		{"any other failure is retried and warned about", "internal", attempts, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nc, up := newUpstream(t, replies{
				"users": func(int) any { return map[string]string{"error": "user account not found", "code": tc.code} },
			})
			store := newMemStore(projection.HydrationState{})
			core, logs := observer.New(zapcore.InfoLevel)
			h := hydration.New(store, nc, up.subjects, queryTTL, liveTTL, 1, zap.New(core))

			h.EnsureAsync(42, hydration.Seed{})

			assert.NotContains(t, store.collect(t, 2), "user")
			require.Eventually(t, func() bool { return up.hits["users"].Load() == tc.wantFetches }, 3*time.Second, 10*time.Millisecond)
			require.Never(t, func() bool { return up.hits["users"].Load() > tc.wantFetches }, 500*time.Millisecond, 10*time.Millisecond)
			store.neverWrites(t, 10*time.Millisecond)
			assert.Equal(t, tc.wantWarns, logs.FilterMessage("hydration: section failed").Len())
			assert.Equal(t, tc.wantWarns, logs.Len())
		})
	}
}
