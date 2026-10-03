// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/pkg/idempotency"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seenAll(t *testing.T, store idempotency.Store, keys ...string) {
	t.Helper()
	for _, key := range keys {
		_, err := store.Seen(context.Background(), key, time.Minute)
		require.NoError(t, err)
	}
}

func TestTieredServesRepeatClaimsFromTheLocalCache(t *testing.T) {
	inner := newFakeStore()
	store := idempotency.NewTiered(100, inner)

	seen, _ := store.Seen(context.Background(), "k", time.Minute)
	assert.False(t, seen, "first Seen must be a fresh claim")
	seen, _ = store.Seen(context.Background(), "k", time.Minute)
	assert.True(t, seen, "second Seen must be a duplicate")

	assert.Equal(t, 1, inner.seenCalls(), "second claim must be served locally")
}

func TestTieredReleaseClearsTheLocalClaim(t *testing.T) {
	inner := newFakeStore()
	store := idempotency.NewTiered(100, inner)
	seenAll(t, store, "k")

	require.NoError(t, store.Release(context.Background(), "k"))
	seen, _ := store.Seen(context.Background(), "k", time.Minute)

	assert.False(t, seen, "claim after release must be fresh")
	assert.Equal(t, 2, inner.seenCalls(), "release must clear the local claim")
}

func TestTieredDoesNotCacheFailOpenMisses(t *testing.T) {
	inner := newFakeStore()
	inner.err = errors.New("valkey down")
	store := idempotency.NewTiered(100, inner)

	for range 2 {
		seen, _ := store.Seen(context.Background(), "k", time.Minute)
		assert.False(t, seen, "fail-open Seen must report not-seen")
	}

	assert.Equal(t, 2, inner.seenCalls(), "fail-open misses must not be cached")
}

func TestTieredEvictsTheLeastRecentlyUsedClaim(t *testing.T) {
	tests := []struct {
		name      string
		claims    []string
		evicted   string
		stillHeld string
	}{
		{name: "evicts the oldest claim at capacity", claims: []string{"a", "b", "c"}, evicted: "a", stillHeld: "b"},
		{name: "keeps a recently used claim", claims: []string{"a", "b", "a", "c"}, evicted: "b", stillHeld: "a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inner := newFakeStore()
			store := idempotency.NewTiered(2, inner)
			seenAll(t, store, tc.claims...)
			before := inner.seenCalls()

			seenAll(t, store, tc.stillHeld)
			assert.Equal(t, before, inner.seenCalls(), "%s must still be cached", tc.stillHeld)

			seenAll(t, store, tc.evicted)
			assert.Equal(t, before+1, inner.seenCalls(), "%s must have been evicted", tc.evicted)
		})
	}
}

func TestTieredForgetsClaimsAfterTheirTTL(t *testing.T) {
	inner := newFakeStore()
	store := idempotency.NewTiered(8, inner)
	_, err := store.Seen(context.Background(), "k", 20*time.Millisecond)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		before := inner.seenCalls()
		_, _ = store.Seen(context.Background(), "k", 20*time.Millisecond)
		return inner.seenCalls() > before
	}, time.Second, 10*time.Millisecond, "expired claim must be rechecked against the inner store")
}
