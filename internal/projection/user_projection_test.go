// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"reflect"
	"testing"

	"ItsBagelBot/internal/domain/event/data"
	contract "ItsBagelBot/internal/domain/rpc/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fullUser = UserProjection{
	StateRevision:      7,
	AccountCreatedAt:   101,
	Status:             "paid",
	IsActive:           true,
	Banned:             true,
	Locale:             "fr",
	CommandsPageHidden: true,
}

func TestFullUserSetsEveryField(t *testing.T) {
	v := reflect.ValueOf(fullUser)
	for i := range v.NumField() {
		require.False(t, v.Field(i).IsZero(), "fullUser leaves %s zero", v.Type().Field(i).Name)
	}
}

func TestUserFromReplyCopiesEveryField(t *testing.T) {
	got := UserFromReply(contract.UserReply{
		StateRevision:      7,
		AccountCreatedAt:   101,
		UserID:             "42",
		Status:             "paid",
		IsActive:           true,
		Banned:             true,
		Locale:             "fr",
		CommandsPageHidden: true,
	})
	require.Equal(t, fullUser, got)
}

func TestUserFromChangedCopiesEveryField(t *testing.T) {
	got := UserFromChanged(data.UserChangedDTO{
		StateRevision:      7,
		UserID:             42,
		AccountCreatedAt:   101,
		Username:           "bagel",
		IsActive:           true,
		Status:             "paid",
		Banned:             true,
		Locale:             "fr",
		CommandsPageHidden: true,
	})
	require.Equal(t, fullUser, got)
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
