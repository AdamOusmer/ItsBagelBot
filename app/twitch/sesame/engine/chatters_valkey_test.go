// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestValkeyChattersStoreAndSnapshotRoundTrip(t *testing.T) {
	f := newChattersFake(t)
	store := NewValkeyChatters(f.client, zap.NewNop())
	ctx := context.Background()

	entries, ok, err := store.Snapshot(ctx, 123)
	require.NoError(t, err)
	assert.False(t, ok, "nothing stored yet")
	assert.Nil(t, entries)

	want := []chattersSnapshotEntry{{ID: 1, Login: "sam", Name: "sam"}, {ID: 2, Login: "alex", Name: "alex"}}
	store.Store(ctx, 123, want)

	got, ok, err := store.Snapshot(ctx, 123)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, want, got)
}

// Past chattersSnapshotCap, Store keeps only the first N entries rather than
// bloating the shared key with a mega-channel's full list.
func TestValkeyChattersStoreCapsTheStoredList(t *testing.T) {
	f := newChattersFake(t)
	store := NewValkeyChatters(f.client, zap.NewNop())
	ctx := context.Background()

	entries := make([]chattersSnapshotEntry, chattersSnapshotCap+50)
	for i := range entries {
		entries[i] = chattersSnapshotEntry{ID: uint64(i + 1), Login: "v", Name: "v"}
	}
	store.Store(ctx, 123, entries)

	got, ok, err := store.Snapshot(ctx, 123)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Len(t, got, chattersSnapshotCap)
}

// The stored key expires on the tick's own schedule (chattersSnapshotTTL =
// watchTickInterval + watchTickJitter), not a shorter, independently-tuned
// number — see the decision record on the constant.
func TestValkeyChattersStoreExpiresOnTheTickSchedule(t *testing.T) {
	f := newChattersFake(t)
	store := NewValkeyChatters(f.client, zap.NewNop())
	ctx := context.Background()
	store.Store(ctx, 123, []chattersSnapshotEntry{{ID: 1, Login: "sam", Name: "sam"}})

	f.advance(chattersSnapshotTTL - time.Second)
	_, ok, err := store.Snapshot(ctx, 123)
	require.NoError(t, err)
	assert.True(t, ok, "still inside the TTL window")

	f.advance(2 * time.Second)
	_, ok, err = store.Snapshot(ctx, 123)
	require.NoError(t, err)
	assert.False(t, ok, "past the TTL window")
}

// Snapshot's ok=false covers a clean miss (err=nil, the normal case a caller
// never logs) and a real Valkey failure (err set) differently, so ViewerRPC
// can decide whether to log — see logDownOnce.
func TestValkeyChattersSnapshotDistinguishesACleanMissFromADownRead(t *testing.T) {
	f := newChattersFake(t)
	store := NewValkeyChatters(f.client, zap.NewNop())
	ctx := context.Background()

	_, ok, err := store.Snapshot(ctx, 123)
	assert.False(t, ok)
	assert.NoError(t, err, "an absent key is a clean miss, not a failure")

	f.breakGET()
	_, ok, err = store.Snapshot(ctx, 123)
	assert.False(t, ok)
	assert.Error(t, err, "a transport failure must be distinguishable from a miss")
}

// A garbled value (Valkey holds something this reader cannot trust) is
// reported as an error too, the same as a transport failure — not a silent
// empty answer.
func TestValkeyChattersSnapshotReportsADecodeFailure(t *testing.T) {
	f := newChattersFake(t)
	store := NewValkeyChatters(f.client, zap.NewNop())
	ctx := context.Background()
	require.NoError(t, f.client.Do(ctx, f.client.B().Set().Key(chattersSnapshotKey(123)).Value("not json").Build()).Error())

	_, ok, err := store.Snapshot(ctx, 123)
	assert.False(t, ok)
	assert.Error(t, err)
}

// TryFetchLock is a cross-replica SET NX: only the first caller wins, and
// ReleaseFetchLock frees it early for a retry rather than waiting out the
// full TTL.
func TestValkeyChattersFetchLockContentionAndRelease(t *testing.T) {
	f := newChattersFake(t)
	store := NewValkeyChatters(f.client, zap.NewNop())
	ctx := context.Background()

	assert.True(t, store.TryFetchLock(ctx, 123), "the first caller wins the lock")
	assert.False(t, store.TryFetchLock(ctx, 123), "a second caller must not win the same lock")

	store.ReleaseFetchLock(ctx, 123)
	assert.True(t, store.TryFetchLock(ctx, 123), "a released lock is claimable again immediately")
}
