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

func newTestViewerRPC(f *fakeValkey, request func(context.Context, manage.ChattersRequest) (manage.ChattersReply, error)) *ViewerRPC {
	return &ViewerRPC{
		store:        NewValkeyChatters(f.client, zap.NewNop()),
		request:      request,
		log:          zap.NewNop(),
		missingScope: make(map[uint64]bool),
		down:         make(map[uint64]bool),
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	require.Eventually(t, cond, 2*time.Second, 2*time.Millisecond)
}

func TestViewerRPCColdSnapshotFetchesOnceUnderContention(t *testing.T) {
	f := newFakeValkey(t)
	var calls atomic.Int32
	release := make(chan struct{})
	r := newTestViewerRPC(f, func(context.Context, manage.ChattersRequest) (manage.ChattersReply, error) {
		calls.Add(1)
		<-release
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
	waitFor(t, func() bool { return calls.Load() == 1 })
	close(release)

	assert.Equal(t, []viewerSnapshotState{viewerSnapshotCold, viewerSnapshotCold}, states, "neither caller waits on the fetch")
	assert.Never(t, func() bool { return calls.Load() > 1 }, 50*time.Millisecond, 5*time.Millisecond,
		"the second caller must not have won a fetch of its own")
}

func TestViewerRPCColdFetchWarmsTheSnapshotForTheNextRead(t *testing.T) {
	f := newFakeValkey(t)
	r := newTestViewerRPC(f, func(context.Context, manage.ChattersRequest) (manage.ChattersReply, error) {
		return manage.ChattersReply{Chatters: []manage.Chatter{{ID: "42", Login: "sam"}, {ID: "7", Login: "alex"}}}, nil
	})

	entries, state := r.Snapshot(context.Background(), 123)
	assert.Equal(t, viewerSnapshotCold, state)
	assert.Nil(t, entries)

	waitFor(t, func() bool {
		_, ok, _ := r.store.Snapshot(context.Background(), 123)
		return ok
	})

	entries, state = r.Snapshot(context.Background(), 123)
	require.Equal(t, viewerSnapshotOK, state)
	assert.ElementsMatch(t,
		[]chattersSnapshotEntry{{ID: 42, Login: "sam", Name: "sam"}, {ID: 7, Login: "alex", Name: "alex"}},
		entries)
}

func TestViewerRPCMissingScopeLatchIsClearedByAWarmSnapshot(t *testing.T) {
	f := newFakeValkey(t)
	r := newTestViewerRPC(f, func(context.Context, manage.ChattersRequest) (manage.ChattersReply, error) {
		return manage.ChattersReply{MissingScope: true}, nil
	})

	_, state := r.Snapshot(context.Background(), 123)
	assert.Equal(t, viewerSnapshotCold, state, "the triggering call itself is still just cold")
	waitFor(t, func() bool {
		_, latched := r.Snapshot(context.Background(), 123)
		return latched == viewerSnapshotMissingScope
	})

	waitFor(t, func() bool { return r.store.TryFetchLock(context.Background(), 123) })
	r.store.ReleaseFetchLock(context.Background(), 123)

	_, state = r.Snapshot(context.Background(), 123)
	assert.Equal(t, viewerSnapshotMissingScope, state, "latched: no fetch attempted while it holds")

	r.store.Store(context.Background(), 123, []chattersSnapshotEntry{{ID: 9, Login: "back", Name: "back"}})

	entries, state := r.Snapshot(context.Background(), 123)
	require.Equal(t, viewerSnapshotOK, state)
	assert.Equal(t, []chattersSnapshotEntry{{ID: 9, Login: "back", Name: "back"}}, entries)

	f.advance(chattersSnapshotTTL + time.Second)
	_, state = r.Snapshot(context.Background(), 123)
	assert.Equal(t, viewerSnapshotCold, state, "a snapshot hit must clear the latch")
}

func TestViewerRPCReleasesTheFetchLockOnRPCFailure(t *testing.T) {
	f := newFakeValkey(t)
	r := newTestViewerRPC(f, func(context.Context, manage.ChattersRequest) (manage.ChattersReply, error) {
		return manage.ChattersReply{}, errors.New("boom")
	})

	_, state := r.Snapshot(context.Background(), 123)
	assert.Equal(t, viewerSnapshotCold, state)

	waitFor(t, func() bool { return r.store.TryFetchLock(context.Background(), 123) })
}
