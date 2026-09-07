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

	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{
		GuildID: "g1", BroadcasterID: 42, InstalledBy: "u1",
	}))

	broadcasterID, found, err := repo.BindingGet(ctx, "g1")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, uint64(42), broadcasterID)

	guilds, err := repo.BindingListByBroadcaster(ctx, 42)
	require.NoError(t, err)
	require.Len(t, guilds, 1)
	assert.Equal(t, "g1", guilds[0].GuildID)
	assert.Equal(t, "u1", guilds[0].InstalledBy)
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

func TestBindingSetIsIdempotentForTheSamePair(t *testing.T) {
	repo, ctx := newStore(t, "bindingidempotent")
	params := repository.BindParams{GuildID: "g1", BroadcasterID: 42, InstalledBy: "u1"}

	require.NoError(t, repo.BindingSet(ctx, params))
	params.InstalledBy = "u2"
	require.NoError(t, repo.BindingSet(ctx, params))

	broadcasterID, found, err := repo.BindingGet(ctx, "g1")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, uint64(42), broadcasterID)
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

// One broadcaster owns many guilds: binding a second server is ordinary, not a
// refusal. This is the inverse of the assertion this test used to make, when
// broadcaster_id carried a unique index.
func TestBindingSetAllowsManyGuildsPerBroadcaster(t *testing.T) {
	repo, ctx := newStore(t, "bindingmanyguilds")

	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g1", BroadcasterID: 42}))
	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g2", BroadcasterID: 42}))

	guilds, err := repo.BindingListByBroadcaster(ctx, 42)
	require.NoError(t, err)
	require.Len(t, guilds, 2)
	assert.Equal(t, "g1", guilds[0].GuildID, "oldest binding first")
	assert.Equal(t, "g2", guilds[1].GuildID)
}

func TestBindingListByBroadcasterRejectsZero(t *testing.T) {
	repo, ctx := newStore(t, "bindinglistinvalid")

	_, err := repo.BindingListByBroadcaster(ctx, 0)
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
}

func TestBindingSetRejectsEmptyInput(t *testing.T) {
	repo, ctx := newStore(t, "bindinginvalid")

	assert.ErrorIs(t, repo.BindingSet(ctx, repository.BindParams{BroadcasterID: 1}), repository.ErrInvalidInput)
	assert.ErrorIs(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g1"}), repository.ErrInvalidInput)
}

func TestBindingDeleteIsGuardedAndIdempotent(t *testing.T) {
	repo, ctx := newStore(t, "bindingdelete")

	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g1", BroadcasterID: 42}))

	// A stale unbind naming the wrong broadcaster must not drop the row.
	assert.ErrorIs(t, repo.BindingDelete(ctx, "g1", 43), repository.ErrBoundElsewhere)
	_, found, err := repo.BindingGet(ctx, "g1")
	require.NoError(t, err)
	assert.True(t, found)

	require.NoError(t, repo.BindingDelete(ctx, "g1", 42))
	_, found, err = repo.BindingGet(ctx, "g1")
	require.NoError(t, err)
	assert.False(t, found)

	// Deleting an absent binding is success: the goal state already holds.
	require.NoError(t, repo.BindingDelete(ctx, "g1", 42))
}

func TestBindingDeleteFreesTheBroadcasterForAnotherGuild(t *testing.T) {
	repo, ctx := newStore(t, "bindingrebind")

	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g1", BroadcasterID: 42}))
	require.NoError(t, repo.BindingDelete(ctx, "g1", 0))
	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: "g2", BroadcasterID: 42}))

	guilds, err := repo.BindingListByBroadcaster(ctx, 42)
	require.NoError(t, err)
	require.Len(t, guilds, 1)
	assert.Equal(t, "g2", guilds[0].GuildID)
}
