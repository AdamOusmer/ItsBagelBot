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

		applied, err = s.SetLive(ctx, id, 1500)
		require.NoError(t, err)
		assert.False(t, applied, "stale online must lose to the deleted-but-remembered offline")
		_, ok = get(t, id)
		assert.False(t, ok)

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

		require.NoError(t, client.Do(ctx, client.B().Set().Key(liveKey(id)).Value("1").ExSeconds(60).Build()).Error())

		applied, err := s.SetLive(ctx, id, 5000)
		require.NoError(t, err)
		assert.True(t, applied)
		val, ok := get(t, id)
		assert.True(t, ok)
		assert.Equal(t, "5000", val)
	})
}
