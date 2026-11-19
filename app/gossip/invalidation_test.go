// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/config"
	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/providertest"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/require"
)

type deadlineDeleteStore struct {
	core.Store
	deadline time.Time
	bounded  bool
}

func (s *deadlineDeleteStore) Del(ctx context.Context, key string) error {
	s.deadline, s.bounded = ctx.Deadline()
	return s.Store.Del(ctx, key)
}

func TestSpotifyInvalidationBoundsTheDelete(t *testing.T) {
	store := &deadlineDeleteStore{Store: providertest.NewMemStore()}
	h := bus.NewInvalidations(nil, "", nil)
	watchSpotify(context.Background(), providertest.Deps(store), h)
	require.NoError(t, h.Publish(invalidate.ScopeSpotify, 42))
	require.True(t, store.bounded)
	require.WithinDuration(t, time.Now().Add(5*time.Second), store.deadline, time.Second)
}

func TestSpotifyInvalidationEvictsOnlyTheBroadcastersToken(t *testing.T) {
	deps := providertest.Deps(providertest.NewMemStore())
	h := bus.NewInvalidations(nil, "", nil)
	watchSpotify(t.Context(), deps, h)
	keys := []string{core.Key("spotify", "token", "42"), core.Key("spotify", "token", "7"), core.Key("other", "data", "42")}
	for _, key := range keys {
		require.NoError(t, deps.Cache.SetJSON(t.Context(), key, "cached", time.Hour))
	}
	require.NoError(t, h.Publish(invalidate.ScopeSpotify, 42))
	for i, key := range keys {
		found, err := deps.Cache.Exists(t.Context(), key)
		require.NoError(t, err)
		require.Equal(t, i != 0, found, key)
	}
}

func TestSpotifyWireInvalidationEvictsTheToken(t *testing.T) {
	nc := testnats.Connect(t)
	deps := providertest.Deps(providertest.NewMemStore())
	h := bus.NewInvalidations(nc, "cache.invalidate", nil)
	watchSpotify(t.Context(), deps, h)
	key := core.Key("spotify", "token", "42")
	require.NoError(t, deps.Cache.SetJSON(t.Context(), key, "cached", time.Hour))
	require.NoError(t, nc.Flush())
	remote := bus.NewInvalidations(nc, "cache.invalidate", nil)
	require.NoError(t, remote.Publish(invalidate.ScopeSpotify, 42))
	require.Eventually(t, func() bool {
		found, err := deps.Cache.Exists(t.Context(), key)
		return err == nil && !found
	}, time.Second, time.Millisecond)
}

func TestGossipProjectionUsesTheSharedInvalidationHub(t *testing.T) {
	vc := valkeytest.Client(t)
	nc := testnats.Connect(t)
	h := bus.NewInvalidations(nc, "cache.invalidate", nil)
	w := gossipWiring{cfg: &config.Config{FetchProjectionSubject: "rpc.test.fetches"}, nc: nc, vc: vc, invalidations: h}
	deps := buildDeps(w)
	t.Cleanup(deps.FetchDefs.(fetchDefSource).client.Close)
	store := projection.NewStore(vc)
	for _, target := range []string{"https://example.com/old", "https://example.com/new"} {
		require.NoError(t, store.SetFetches(t.Context(), 42, []projection.FetchView{{Name: "weather", URL: target, IsActive: true}}))
		require.NoError(t, h.Publish(invalidate.ScopeFetches, 42, "weather"))
		got, found, err := deps.FetchDefs.FetchDef(context.Background(), "42", "weather")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, target, got.URL)
	}
}
