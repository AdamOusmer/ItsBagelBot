// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestUserDeletionInvalidatesEveryContentScopeAfterProjectionRemoval(t *testing.T) {
	store := projection.NewStore(testValkey(t))
	h := bus.NewInvalidations(nil, "", nil)
	p := NewProjector(Deps{Store: store, Invalidations: h, Log: zap.NewNop()})
	require.NoError(t, store.SetUser(t.Context(), 1001, projection.UserProjection{AccountCreatedAt: 100, Status: "premium", IsActive: true}))
	want := []string{invalidate.ScopeUserProjection, invalidate.ScopeModules, invalidate.ScopeCommands, invalidate.ScopeFetches, invalidate.ScopeAutomations}
	var got []string
	for _, scope := range want {
		h.Watch(scope, func(e cache.Event) {
			_, found, err := store.GetUserRuntime(t.Context(), e.ID)
			require.NoError(t, err)
			require.False(t, found)
			got = append(got, e.Scope)
		})
	}
	require.NoError(t, p.HandleUserDeleted(message(t, "delete-content", data.UserDeletedDTO{UserID: 1001, AccountCreatedAt: 100})))
	require.ElementsMatch(t, want, got)
}

func TestFetchChangeInvalidatesOnlyAfterProjectionWrite(t *testing.T) {
	store := projection.NewStore(testValkey(t))
	h := bus.NewInvalidations(nil, "", nil)
	p := NewProjector(Deps{Store: store, Invalidations: h, Log: zap.NewNop()})
	var urls []string
	h.Watch(invalidate.ScopeFetches, func(e cache.Event) {
		view, _, _, err := store.GetFetch(t.Context(), e.ID, "weather")
		require.NoError(t, err)
		urls = append(urls, view.URL)
	})
	for _, dto := range []data.FetchChangedDTO{
		{UserID: 1001, Name: "weather", URL: "https://api.example.com", IsActive: true},
		{UserID: 1001, Name: "weather", Deleted: true},
	} {
		require.NoError(t, p.HandleFetchChanged(message(t, "fetch-change", dto)))
	}
	require.Equal(t, []string{"https://api.example.com", ""}, urls)
}

func TestUserChangeInvalidatesAfterProjectionWrite(t *testing.T) {
	store := projection.NewStore(testValkey(t))
	h := bus.NewInvalidations(nil, "", nil)
	p := NewProjector(Deps{Store: store, Invalidations: h, Log: zap.NewNop()})
	var statuses []string
	h.Watch(invalidate.ScopeUserProjection, func(e cache.Event) {
		view, found, err := store.GetUserRuntime(t.Context(), e.ID)
		require.NoError(t, err)
		require.True(t, found)
		statuses = append(statuses, view.Status)
	})
	require.NoError(t, p.HandleUserChanged(message(t, "user-change", data.UserChangedDTO{UserID: 1001, Status: "paid", IsActive: true})))
	require.Equal(t, []string{"paid"}, statuses)
}
