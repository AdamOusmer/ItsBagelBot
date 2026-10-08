// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/pkg/idempotency"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValkeyStoreClaimsOnceAndReleases(t *testing.T) {
	fake := newFakeValkey(t)
	store := idempotency.NewValkeyStore(fake.client, "idem:", nil)
	ctx := context.Background()

	seen, err := store.Seen(ctx, "k", time.Minute)
	require.NoError(t, err)
	assert.False(t, seen, "first claim must be fresh")

	seen, err = store.Seen(ctx, "k", time.Minute)
	require.NoError(t, err)
	assert.True(t, seen, "second claim must be a duplicate")

	require.NoError(t, store.Release(ctx, "k"))
	seen, err = store.Seen(ctx, "k", time.Minute)
	require.NoError(t, err)
	assert.False(t, seen, "claim after release must be fresh")
	assert.Zero(t, store.FailOpenCount())
}

func TestValkeyStoreSendsAtLeastOneMillisecondTTL(t *testing.T) {
	tests := []struct {
		name   string
		ttl    time.Duration
		wantPX string
	}{
		{name: "floors a zero ttl to one millisecond", ttl: 0, wantPX: "1"},
		{name: "floors a sub-millisecond ttl to one millisecond", ttl: 500 * time.Microsecond, wantPX: "1"},
		{name: "sends whole milliseconds", ttl: 2 * time.Second, wantPX: "2000"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeValkey(t)

			_, err := idempotency.NewValkeyStore(fake.client, "idem:", nil).Seen(context.Background(), "k", tc.ttl)

			require.NoError(t, err)
			assert.Equal(t, tc.wantPX, fake.lastSetPX())
		})
	}
}

func TestValkeyStoreFailsOpenOnInfraErrors(t *testing.T) {
	fake := newFakeValkey(t)
	store := idempotency.NewValkeyStore(fake.client, "idem:", nil)
	fake.breakServer()

	seen, err := store.Seen(context.Background(), "k", time.Minute)

	require.Error(t, err)
	assert.False(t, seen, "an infra error must never read as a duplicate")
	assert.Equal(t, int64(1), store.FailOpenCount())
}
