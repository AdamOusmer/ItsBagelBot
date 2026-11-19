// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/ent/enttest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/providertest"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func TestSpotifyRotationKeepsMintedAccessTokenCached(t *testing.T) {
	client := testdb.Open(t, "spotify-rotation", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	h := bus.NewInvalidations(nil, "", nil)
	repo := repository.NewSpotifyCreds(client, dbtest.NewPacker(t), h)
	require.NoError(t, repo.SetToken(t.Context(), 1001, repository.SpotifyGrant{RefreshToken: "before"}))
	deps := providertest.Deps(providertest.NewMemStore())
	watchSpotify(t.Context(), deps, h)
	mints := 0
	for range 2 {
		token, err := core.CachedBytes(t.Context(), deps.Cache, core.Key("spotify", "token", "1001"), nil, func(ctx context.Context) ([]byte, time.Duration, error) {
			mints++
			return []byte("access-token"), time.Hour, repo.RotateToken(ctx, 1001, "before", "next")
		})
		require.NoError(t, err)
		require.Equal(t, "access-token", string(token))
	}
	require.Equal(t, 1, mints)
	stored, err := repo.Token(t.Context(), 1001)
	require.NoError(t, err)
	require.Equal(t, "next", stored)
}
