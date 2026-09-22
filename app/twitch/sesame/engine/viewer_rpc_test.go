// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/rpc/manage"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// newTestViewerRPC builds a ViewerRPC over a real ValkeyChatters (fronting
// the fake Valkey server) and an injected request func, the same
// bypass-the-NATS-constructor shape uptime_rpc_test.go uses for UptimeRPC:
// this package's tests can set unexported fields directly, so there is no
// need for an embedded NATS server just to reach ViewerRPC's Valkey-backed
// behavior.
func newTestViewerRPC(f *chattersFake, request func(context.Context, manage.ChattersRequest) (manage.ChattersReply, error)) *ViewerRPC {
	return &ViewerRPC{
		store:        NewValkeyChatters(f.client, zap.NewNop()),
		request:      request,
		log:          zap.NewNop(),
		missingScope: make(map[uint64]bool),
		down:         make(map[uint64]bool),
	}
}

// waitFor polls cond for up to a small, generous budget: fetch runs in its
// own goroutine (Snapshot never blocks on it, by design), so a test proving
// what that goroutine eventually does has nothing to synchronously wait on
// except the Valkey key it writes.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition never became true")
}

// A cold snapshot triggers exactly one background fetch, however many
// callers race the same cold cache: ValkeyChatters' cross-replica lock
// blocks every caller after the first from even trying, and the loser
// answers cold for its own run rather than fetching a second time.
func TestViewerRPCColdSnapshotFetchesOnceUnderContention(t *testing.T) {
	f := newChattersFake(t)
	var calls atomic.Int32
	r := newTestViewerRPC(f, func(context.Context, manage.ChattersRequest) (manage.ChattersReply, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond) // wide enough for both Snapshot calls to race it
		return manage.ChattersReply{Chatters: []manage.Chatter{{ID: "42", Login: "sam"}}}, nil
	})

	var wg sync.WaitGroup
	states := make([]viewerSnapshotState, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, states[i] = r.Snapshot(context.Background(), 123)
		}(i)
	}
	wg.Wait()

	assert.Equal(t, []viewerSnapshotState{viewerSnapshotCold, viewerSnapshotCold}, states, "neither caller waits on the fetch")
	waitFor(t, func() bool { return calls.Load() == 1 })
	time.Sleep(30 * time.Millisecond) // outlast the fetch, then confirm it never fired twice
	assert.Equal(t, int32(1), calls.Load(), "the second caller must not have won a fetch of its own")
}

// The full lazy-fetch path: a cold Snapshot fires a background fetch, which
// Stores the result, which the NEXT Snapshot then reads as a hit.
func TestViewerRPCColdFetchWarmsTheSnapshotForTheNextRead(t *testing.T) {
	f := newChattersFake(t)
	r := newTestViewerRPC(f, func(context.Context, manage.ChattersRequest) (manage.ChattersReply, error) {
		return manage.ChattersReply{Chatters: []manage.Chatter{{ID: "42", Login: "sam"}, {ID: "7", Login: "alex"}}}, nil
	})

	entries, state := r.Snapshot(context.Background(), 123)
	assert.Equal(t, viewerSnapshotCold, state)
	assert.Nil(t, entries)

	waitFor(t, func() bool {
		_, ok := f.rawValue(chattersSnapshotKey(123))
		return ok
	})

	entries, state = r.Snapshot(context.Background(), 123)
	require.Equal(t, viewerSnapshotOK, state)
	assert.ElementsMatch(t,
		[]chattersSnapshotEntry{{ID: 42, Login: "sam", Name: "sam"}, {ID: 7, Login: "alex", Name: "alex"}},
		entries)
}

// A MissingScope reply latches the channel (Snapshot skips fetching for it
// from then on) AND releases the fetch lock early, so it does not sit
// claimed for the lock's full TTL. Once the shared snapshot is warmed some
// other way (the loyalty tick sharing the same verb), the very next Snapshot
// clears the latch on its own — no restart required.
func TestViewerRPCMissingScopeLatchIsClearedByAWarmSnapshot(t *testing.T) {
	f := newChattersFake(t)
	r := newTestViewerRPC(f, func(context.Context, manage.ChattersRequest) (manage.ChattersReply, error) {
		return manage.ChattersReply{MissingScope: true}, nil
	})

	_, state := r.Snapshot(context.Background(), 123)
	assert.Equal(t, viewerSnapshotCold, state, "the triggering call itself is still just cold")
	waitFor(t, func() bool { return r.isMissingScope(123) })

	assert.True(t, r.store.TryFetchLock(context.Background(), 123), "MissingScope must have released the fetch lock early")
	r.store.ReleaseFetchLock(context.Background(), 123) // undo the probe claim above

	_, state = r.Snapshot(context.Background(), 123)
	assert.Equal(t, viewerSnapshotMissingScope, state, "latched: no fetch attempted while it holds")

	// The tick (or anything else sharing chatters.get) repopulates the shared
	// snapshot independently of this latch.
	r.store.Store(context.Background(), 123, []chattersSnapshotEntry{{ID: 9, Login: "back", Name: "back"}})

	entries, state := r.Snapshot(context.Background(), 123)
	require.Equal(t, viewerSnapshotOK, state)
	assert.Equal(t, []chattersSnapshotEntry{{ID: 9, Login: "back", Name: "back"}}, entries)
	assert.False(t, r.isMissingScope(123), "a snapshot hit must clear the latch")
}

// An RPC-level failure (transport error, or a reply carrying Error) also
// releases the fetch lock early, the same as MissingScope, so a transient
// failure does not block a retry for the lock's full TTL.
func TestViewerRPCReleasesTheFetchLockOnRPCFailure(t *testing.T) {
	f := newChattersFake(t)
	r := newTestViewerRPC(f, func(context.Context, manage.ChattersRequest) (manage.ChattersReply, error) {
		return manage.ChattersReply{}, errors.New("boom")
	})

	_, state := r.Snapshot(context.Background(), 123)
	assert.Equal(t, viewerSnapshotCold, state)

	waitFor(t, func() bool { return r.store.TryFetchLock(context.Background(), 123) })
}
