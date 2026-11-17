// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"strconv"
	"testing"

	"ItsBagelBot/app/db/modules/ent"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/require"
)

func TestSpotifyChangesInvalidateAfterCommit(t *testing.T) {
	cases := []struct {
		name  string
		write func(context.Context, *repository.SpotifyCreds) error
		want  repository.SpotifyGrantStatus
	}{
		{name: "app", write: func(ctx context.Context, repo *repository.SpotifyCreds) error {
			return repo.SetApp(ctx, 1001, repository.SpotifyApp{ClientID: "new-app", ClientSecret: "new-secret"})
		}, want: repository.SpotifyGrantStatus{Present: true, Scopes: []string{}}},
		{name: "token", write: func(ctx context.Context, repo *repository.SpotifyCreds) error {
			return repo.SetToken(ctx, 1001, repository.SpotifyGrant{RefreshToken: "next"})
		}, want: repository.SpotifyGrantStatus{Present: true, Scopes: []string{}}},
		{name: "dead", write: func(ctx context.Context, repo *repository.SpotifyCreds) error {
			return repo.MarkTokenDead(ctx, 1001, "before")
		}, want: repository.SpotifyGrantStatus{Present: true, Scopes: []string{}, NeedsReconnect: true}},
		{name: "clear token", write: func(ctx context.Context, repo *repository.SpotifyCreds) error {
			return repo.ClearToken(ctx, 1001)
		}},
		{name: "clear app", write: func(ctx context.Context, repo *repository.SpotifyCreds) error {
			return repo.ClearApp(ctx, 1001)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := spotifySetup(t)
			h := bus.NewInvalidations(nil, "", nil)
			repo := repository.NewSpotifyCreds(client, dbtest.NewPacker(t), h)
			require.NoError(t, repo.SetToken(t.Context(), 1001, repository.SpotifyGrant{RefreshToken: "before"}))
			var seen []repository.SpotifyGrantStatus
			h.Watch(invalidate.ScopeSpotify, func(e cache.Event) {
				status, err := repo.TokenStatus(t.Context(), repository.SpotifyUserID(e.ID))
				require.NoError(t, err)
				seen = append(seen, status)
			})
			require.NoError(t, tc.write(t.Context(), repo))
			require.Equal(t, []repository.SpotifyGrantStatus{tc.want}, seen)
		})
	}
}

func TestSpotifyZeroRowCASDoesNotInvalidate(t *testing.T) {
	for _, rotation := range []bool{false, true} {
		t.Run(strconv.FormatBool(rotation), func(t *testing.T) {
			client, _ := spotifySetup(t)
			h := bus.NewInvalidations(nil, "", nil)
			repo := repository.NewSpotifyCreds(client, dbtest.NewPacker(t), h)
			require.NoError(t, repo.SetToken(t.Context(), 1001, repository.SpotifyGrant{RefreshToken: "before"}))
			invalidated := false
			h.Watch(invalidate.ScopeSpotify, func(cache.Event) { invalidated = true })
			client.SpotifyCredential.Use(func(next ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(ctx context.Context, m ent.Mutation) (ent.Value, error) {
					if m.Op() == ent.OpUpdate {
						_, err := client.SpotifyCredential.Delete().Exec(ctx)
						require.NoError(t, err)
					}
					return next.Mutate(ctx, m)
				})
			})
			if rotation {
				require.ErrorIs(t, repo.RotateToken(t.Context(), 1001, "before", "next"), repository.ErrRotateStale)
			} else {
				require.NoError(t, repo.MarkTokenDead(t.Context(), 1001, "before"))
			}
			require.False(t, invalidated)
		})
	}
}

func TestUnchangedSpotifyCredentialsDoNotInvalidate(t *testing.T) {
	client, _ := spotifySetup(t)
	h := bus.NewInvalidations(nil, "", nil)
	repo := repository.NewSpotifyCreds(client, dbtest.NewPacker(t), h)
	app := repository.SpotifyApp{ClientID: "app", ClientSecret: "secret"}
	grant := repository.SpotifyGrant{RefreshToken: "token", Scopes: []string{"scope"}}
	require.NoError(t, repo.SetApp(t.Context(), 1001, app))
	require.NoError(t, repo.SetToken(t.Context(), 1001, grant))
	var seen []cache.Event
	h.Watch(invalidate.ScopeSpotify, func(e cache.Event) { seen = append(seen, e) })
	require.NoError(t, repo.SetApp(t.Context(), 1001, app))
	require.NoError(t, repo.SetToken(t.Context(), 1001, grant))
	require.NoError(t, repo.MarkTokenDead(t.Context(), 1001, "stale"))
	require.NoError(t, repo.ClearApp(t.Context(), 999))
	require.NoError(t, repo.ClearToken(t.Context(), 999))
	require.Empty(t, seen)
	require.NoError(t, repo.ClearToken(t.Context(), 1001))
	seen = nil
	require.NoError(t, repo.ClearToken(t.Context(), 1001))
	require.Empty(t, seen)
}
