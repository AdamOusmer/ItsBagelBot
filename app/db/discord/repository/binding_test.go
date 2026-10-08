// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	"ItsBagelBot/app/db/discord/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBindingSetAndGet(t *testing.T) {
	repo, ctx := newStore(t, "bindingset")

	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g1", BroadcasterID: 42, InstalledBy: "u1"}))
	broadcasterID, found, err := repo.BindingGet(ctx, "g1")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, uint64(42), broadcasterID)

	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g1", BroadcasterID: 42, InstalledBy: "u2"}), "the same pair binds idempotently")
	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g2", BroadcasterID: 42}))

	guilds, err := repo.BindingListByBroadcaster(ctx, 42)
	require.NoError(t, err)
	require.Len(t, guilds, 2, "a broadcaster may bind many guilds")
	assert.Equal(t, []string{"g1", "g2"}, []string{guilds[0].GuildID, guilds[1].GuildID}, "oldest binding first")
}

func TestBindingGetMissingIsNotAnError(t *testing.T) {
	repo, ctx := newStore(t, "bindingmissing")

	_, found, err := repo.BindingGet(ctx, "nope")
	require.NoError(t, err)
	assert.False(t, found)

	guilds, err := repo.BindingListByBroadcaster(ctx, 999)
	require.NoError(t, err)
	assert.Empty(t, guilds)
}

func TestBindingSetRefusesAGuildBoundElsewhere(t *testing.T) {
	repo, ctx := newStore(t, "bindingguildtaken")

	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g1", BroadcasterID: 42}))

	err := repo.BindingSet(ctx, repository.BindParams{GuildID: "g1", BroadcasterID: 43})
	assert.ErrorIs(t, err, repository.ErrBoundElsewhere)

	broadcasterID, _, err := repo.BindingGet(ctx, "g1")
	require.NoError(t, err)
	assert.Equal(t, uint64(42), broadcasterID, "the losing bind must not move the row")
}

func TestBindingRejectsEmptyInput(t *testing.T) {
	repo, ctx := newStore(t, "bindinginvalid")

	_, err := repo.BindingListByBroadcaster(ctx, 0)
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
	assert.ErrorIs(t, repo.BindingSet(ctx, repository.BindParams{BroadcasterID: 1}), repository.ErrInvalidInput)
	assert.ErrorIs(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g1"}), repository.ErrInvalidInput)
}

func TestBindingDeleteIsGuardedAndFreesTheBroadcaster(t *testing.T) {
	repo, ctx := newStore(t, "bindingdelete")

	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g1", BroadcasterID: 42}))

	assert.ErrorIs(t, repo.BindingDelete(ctx, "g1", 43), repository.ErrBoundElsewhere)
	_, found, err := repo.BindingGet(ctx, "g1")
	require.NoError(t, err)
	assert.True(t, found)

	require.NoError(t, repo.BindingDelete(ctx, "g1", 42))
	_, found, err = repo.BindingGet(ctx, "g1")
	require.NoError(t, err)
	assert.False(t, found)
	require.NoError(t, repo.BindingDelete(ctx, "g1", 42), "deleting again is idempotent")

	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g3", BroadcasterID: 44}))
	require.NoError(t, repo.BindingDelete(ctx, "g3", 0), "a zero broadcaster deletes unguarded")
	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g4", BroadcasterID: 44}))
	guilds, err := repo.BindingListByBroadcaster(ctx, 44)
	require.NoError(t, err)
	require.Len(t, guilds, 1)
	assert.Equal(t, "g4", guilds[0].GuildID)
}
