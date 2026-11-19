// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"

	"github.com/stretchr/testify/require"
)

type namedContentCase struct {
	scope string
	seed  func(context.Context, uint64, string) error
	read  func(context.Context, uint64) (string, error)
}

func TestKeylessContentInvalidationEvictsOnlyTheDeletedUser(t *testing.T) {
	store, _ := newTestStore(t)
	c, evict := invalidatedClient(t, store)
	cases := []namedContentCase{
		{scope: invalidate.ScopeCommands,
			seed: func(ctx context.Context, id uint64, value string) error {
				return store.SetCommands(ctx, id, []CommandView{{Name: "hello", Aliases: []string{"hi"}, Response: value}})
			},
			read: func(ctx context.Context, id uint64) (string, error) {
				cmd, _, err := c.Command(ctx, id, "hi")
				return cmd.Response, err
			},
		},
		{scope: invalidate.ScopeFetches,
			seed: func(ctx context.Context, id uint64, value string) error {
				return store.SetFetches(ctx, id, []FetchView{{Name: "hello", URL: value}})
			},
			read: func(ctx context.Context, id uint64) (string, error) {
				fetch, _, err := c.FetchDefs(ctx, id, "hello")
				return fetch.URL, err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			for _, id := range []uint64{7, 70} {
				require.NoError(t, tc.seed(t.Context(), id, "before"))
				value, err := tc.read(t.Context(), id)
				require.NoError(t, err)
				require.Equal(t, "before", value)
				require.NoError(t, tc.seed(t.Context(), id, "after"))
			}
			evict(tc.scope, 7)
			assertContentValue(t, tc, 7, "after")
			assertContentValue(t, tc, 70, "before")
		})
	}
}

func assertContentValue(t *testing.T, tc namedContentCase, id uint64, want string) {
	t.Helper()
	value, err := tc.read(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, want, value)
}

func TestPartialFetchProjectionDoesNotNeedOwnerRPC(t *testing.T) {
	store, _ := newTestStore(t)
	c, _ := invalidatedClient(t, store)
	require.NoError(t, store.SetFetch(t.Context(), data.FetchChangedDTO{UserID: 7, Name: "weather", URL: "https://api.example.com", IsActive: true}))
	view, found, err := c.FetchDefs(t.Context(), 7, "weather")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "https://api.example.com", view.URL)
}

func TestUserCacheWaitsForProjectionInvalidation(t *testing.T) {
	store, _ := newTestStore(t)
	c, evict := invalidatedClient(t, store)
	require.NoError(t, store.SetUser(t.Context(), 7, UserProjection{Status: "standard", IsActive: true}))
	before, err := c.User(t.Context(), 7)
	require.NoError(t, err)
	for _, scope := range []string{invalidate.ScopeStatus, invalidate.ScopeGrant, invalidate.ScopeLocale, invalidate.ScopeCommandsPage, invalidate.ScopeLeaderboardPage, invalidate.ScopeSongQueuePage} {
		evict(scope, 7)
	}
	require.NoError(t, store.SetUser(t.Context(), 7, UserProjection{Status: "premium", IsActive: true}))
	window, err := c.User(t.Context(), 7)
	require.NoError(t, err)
	require.Equal(t, before, window)
	evict(invalidate.ScopeUserProjection, 7)
	after, err := c.User(t.Context(), 7)
	require.NoError(t, err)
	require.Equal(t, "premium", after.Status)
}
