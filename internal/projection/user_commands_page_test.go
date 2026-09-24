// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
