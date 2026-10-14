// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/providertest"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func TestValkeyStoreCompressesLargeValuesAndReadsLegacyPlain(t *testing.T) {
	ctx := context.Background()
	client := valkeytest.Client(t)
	store := core.NewValkeyStore(client)

	large := []byte(`{"v":"` + strings.Repeat("gossip payload ", 200) + `","f":1}`)
	require.NoError(t, store.Set(ctx, "gossip:t:large", large, time.Minute))

	raw, err := client.Do(ctx, client.B().Get().Key("gossip:t:large").Build()).AsBytes()
	require.NoError(t, err)
	require.Less(t, len(raw), len(large)/2)

	got, ok, err := store.Get(ctx, "gossip:t:large")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, large, got)

	require.NoError(t, client.Do(ctx, client.B().Set().Key("gossip:t:legacy").Value(string(large)).Build()).Error())
	got, ok, err = store.Get(ctx, "gossip:t:legacy")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, large, got)
}

func TestStoredTTLCapsStaleWindowAtOneDay(t *testing.T) {
	ctx := context.Background()
	store := providertest.NewMemStore()
	cache := core.NewCache(store)

	for key, tc := range map[string]struct{ fresh, retained time.Duration }{
		"short": {time.Minute, 2 * time.Minute},
		"day":   {24 * time.Hour, 48 * time.Hour},
		"long":  {14 * 24 * time.Hour, 15 * 24 * time.Hour},
	} {
		core.StoreCached(ctx, cache, core.StoreRequest[string]{Key: key, TTL: tc.fresh, NegativeTTL: time.Minute, Value: "v"})
		require.Equal(t, tc.retained, store.Retention(key), key)
	}
}

func TestCachedBytesStoredTTLCapsStaleWindowAtOneDay(t *testing.T) {
	ctx := context.Background()
	store := providertest.NewMemStore()
	cache := core.NewCache(store)

	_, err := core.CachedBytes(ctx, cache, "bytes", nil, buildStatic(`{}`, 14*24*time.Hour, nil))
	require.NoError(t, err)
	require.Equal(t, 15*24*time.Hour, store.Retention("bytes"))
}
