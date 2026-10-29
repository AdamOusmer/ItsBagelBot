// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"reflect"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/domain/lifecycle"
	contract "ItsBagelBot/internal/domain/rpc/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fullUser = UserProjection{
	Lifecycle:          &lifecycle.State{Version: 1, DesiredEnabled: true, RuntimeState: lifecycle.Active, ChatRestriction: lifecycle.RestrictionNone, GrantHealth: lifecycle.GrantHealthy, AdmissionGeneration: 3},
	StateRevision:      7,
	AccountCreatedAt:   101,
	Status:             "paid",
	IsActive:           true,
	Banned:             true,
	Locale:             "fr",
	CommandsPageHidden: true,

	LeaderboardPageHidden: true,
	CreatorCode:           "BAGEL10",
}

func TestFullUserSetsEveryField(t *testing.T) {
	v := reflect.ValueOf(fullUser)
	for i := range v.NumField() {
		require.False(t, v.Field(i).IsZero(), "fullUser leaves %s zero", v.Type().Field(i).Name)
	}
}

func TestUserMappersCopyEveryField(t *testing.T) {
	mapped := map[string]UserProjection{
		"reply": UserFromReply(contract.UserReply{
			Lifecycle:          fullUser.Lifecycle,
			StateRevision:      7,
			AccountCreatedAt:   101,
			UserID:             "42",
			Status:             "paid",
			IsActive:           true,
			Banned:             true,
			Locale:             "fr",
			CommandsPageHidden: true,

			LeaderboardPageHidden: true,
			CreatorCode:           "BAGEL10",
		}),
		"changed": UserFromChanged(data.UserChangedDTO{
			Lifecycle:          fullUser.Lifecycle,
			StateRevision:      7,
			UserID:             42,
			AccountCreatedAt:   101,
			Username:           "bagel",
			IsActive:           true,
			Status:             "paid",
			Banned:             true,
			Locale:             "fr",
			CommandsPageHidden: true,

			LeaderboardPageHidden: true,
			CreatorCode:           "BAGEL10",
		}),
	}
	for name, got := range mapped {
		require.Equal(t, fullUser, got, name)
	}
}

func TestSetUserRoundTripsCreatorCode(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.SetUser(ctx, 74, UserProjection{Status: "vip", IsActive: true, CreatorCode: "BAGEL10"}))
	user, found, err := store.GetUserRuntime(ctx, 74)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "BAGEL10", user.CreatorCode)

	require.NoError(t, store.SetUser(ctx, 74, UserProjection{Status: "vip", IsActive: true}))
	user, _, err = store.GetUserRuntime(ctx, 74)
	require.NoError(t, err)
	assert.Empty(t, user.CreatorCode, "a cleared code must overwrite the projected one")
}

func TestSetUserRoundTripsLeaderboardPageHidden(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.SetUser(ctx, 73, UserProjection{Status: "vip", IsActive: true, LeaderboardPageHidden: true}))
	assert.Equal(t, "1", f.hash("settings:73")["leaderboard_page_hidden"])

	require.NoError(t, store.SetUser(ctx, 73, UserProjection{Status: "vip", IsActive: true}))
	assert.Equal(t, "0", f.hash("settings:73")["leaderboard_page_hidden"], "an event without the flag folds to visible")
}

func TestSetUserGetUserRoundTripsCommandsPageHidden(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()

	require.NoError(t, store.SetUser(ctx, 71, UserProjection{
		Status:             "vip",
		IsActive:           true,
		CommandsPageHidden: true,
	}))

	h := f.hash("settings:71")
	assert.Equal(t, "1", h["commands_page_hidden"], "written unconditionally, unlike locale (D2 needs no skip-when-empty rule)")

	status, active, banned, _, commandsPageHidden, err := store.GetUser(ctx, 71)
	require.NoError(t, err)
	assert.Equal(t, "vip", status)
	assert.True(t, active)
	assert.False(t, banned)
	assert.True(t, commandsPageHidden)
}

func TestGetUserAbsentCommandsPageFieldReadsFalse(t *testing.T) {
	store, f := newTestStore(t)
	ctx := context.Background()
	key := "settings:72"

	f.seed(key, fakeField{field: "status", value: "paid"})
	f.seed(key, fakeField{field: "active", value: "1"})

	status, active, _, _, commandsPageHidden, err := store.GetUser(ctx, 72)
	require.NoError(t, err)
	assert.Equal(t, "paid", status)
	assert.True(t, active)
	assert.False(t, commandsPageHidden, "an absent field must resolve to visible, the pre-feature behaviour (D2)")
}
