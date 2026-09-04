// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	livekey "ItsBagelBot/internal/domain/live"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// TestVersionedLiveWrites drives the #561 ordering semantics against a real
// Valkey: the scripts, not the callers, are what keep a stale stream.online
// from resurrecting a key an offline already cleared — including the case the
// plain GET-based check cannot see, where the DEL has already removed the key
// and only the companion ver: key remembers the applied version. Skipped like
// the other hot-path tests when VALKEY_TEST_ADDR is unset.
func TestVersionedLiveWrites(t *testing.T) {
	client := newHotPathTestClient(t)
	ctx := context.Background()
	s := NewValkeyLiveStore(client, nil, nil, LiveConfig{TTL: time.Minute, Log: zap.NewNop()})

	get := func(t *testing.T, id uint64) (string, bool) {
		t.Helper()
		val, err := client.Do(ctx, client.B().Get().Key(liveKey(id)).Build()).ToString()
		if valkey.IsValkeyNil(err) {
			return "", false
		}
		require.NoError(t, err)
		return val, true
	}
	cleanupKeys := func(ids ...uint64) {
		for _, id := range ids {
			_ = client.Do(ctx, client.B().Del().Key(liveKey(id)).Key(livekey.VerKey(id)).Build()).Error()
		}
	}

	t.Run("online then offline then stale online", func(t *testing.T) {
		const id uint64 = 5601
		cleanupKeys(id)
		t.Cleanup(func() { cleanupKeys(id) })

		applied, err := s.SetLive(ctx, id, 1000)
		require.NoError(t, err)
		assert.True(t, applied, "first online must apply")
		val, ok := get(t, id)
		assert.True(t, ok)
		assert.Equal(t, "1000", val, "the value carries the applied version")

		applied, err = s.ClearLive(ctx, id, 2000)
		require.NoError(t, err)
		assert.True(t, applied)
		_, ok = get(t, id)
		assert.False(t, ok, "offline must delete the live key")

		// The resurrection scenario: an older online redelivered after the
		// offline's DEL. No live key exists; only ver: remembers 2000 > 1000.
		applied, err = s.SetLive(ctx, id, 1500)
		require.NoError(t, err)
		assert.False(t, applied, "stale online must lose to the deleted-but-remembered offline")
		_, ok = get(t, id)
		assert.False(t, ok)

		// A genuinely newer online still applies.
		applied, err = s.SetLive(ctx, id, 3000)
		require.NoError(t, err)
		assert.True(t, applied)
	})

	t.Run("equal version applies last writer wins", func(t *testing.T) {
		const id uint64 = 5602
		cleanupKeys(id)
		t.Cleanup(func() { cleanupKeys(id) })

		applied, err := s.SetLive(ctx, id, 1000)
		require.NoError(t, err)
		assert.True(t, applied)
		applied, err = s.ClearLive(ctx, id, 1000)
		require.NoError(t, err)
		assert.True(t, applied, "an equal-version offline may clear")
	})

	t.Run("legacy constant value is superseded without a flush", func(t *testing.T) {
		const id uint64 = 5603
		cleanupKeys(id)
		t.Cleanup(func() { cleanupKeys(id) })

		// A pre-versioning replica wrote the old constant "1".
		require.NoError(t, client.Do(ctx, client.B().Set().Key(liveKey(id)).Value("1").ExSeconds(60).Build()).Error())

		// No ver: claim exists yet, so a versioned online takes over cleanly.
		applied, err := s.SetLive(ctx, id, 5000)
		require.NoError(t, err)
		assert.True(t, applied)
		val, ok := get(t, id)
		assert.True(t, ok)
		assert.Equal(t, "5000", val)
	})
}
