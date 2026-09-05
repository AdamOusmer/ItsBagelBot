// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/discord/repository"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bindOne binds one guild so the config paths under test have an owner.
func bindOne(t *testing.T, repo *repository.Store, ctx context.Context, guildID string, broadcasterID uint64) {
	t.Helper()
	require.NoError(t, repo.BindingSet(ctx, repository.BindParams{GuildID: guildID, BroadcasterID: broadcasterID}))
}

func TestConfigGetMissingIsNotAnError(t *testing.T) {
	repo, ctx := newStore(t, "configmissing")

	cfg, version, found, err := repo.ConfigGet(ctx, "g1")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Zero(t, version)
	assert.Equal(t, ddiscord.Config{}, cfg)
}

func TestConfigSetCreatesThenUpdates(t *testing.T) {
	repo, ctx := newStore(t, "configcreate")
	bindOne(t, repo, ctx, "g1", 42)

	version, err := repo.ConfigSet(ctx, repository.SetConfigParams{
		GuildID: "g1", BroadcasterID: 42,
		Config: ddiscord.Config{GuildID: "g1", LiveChannelID: "123", LiveEnabled: "on"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, version)

	cfg, version, found, err := repo.ConfigGet(ctx, "g1")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, 1, version)
	assert.Equal(t, "123", cfg.LiveChannelID)
	assert.True(t, cfg.LiveOn())

	version, err = repo.ConfigSet(ctx, repository.SetConfigParams{
		GuildID: "g1", BroadcasterID: 42, ExpectedVersion: 1,
		Config: ddiscord.Config{GuildID: "g1", LiveChannelID: "456"},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, version)

	cfg, _, _, err = repo.ConfigGet(ctx, "g1")
	require.NoError(t, err)
	assert.Equal(t, "456", cfg.LiveChannelID)
}

func TestConfigSetRefusesAStaleVersion(t *testing.T) {
	repo, ctx := newStore(t, "configconflict")
	bindOne(t, repo, ctx, "g1", 42)

	_, err := repo.ConfigSet(ctx, repository.SetConfigParams{GuildID: "g1", BroadcasterID: 42})
	require.NoError(t, err)

	// The second tab still holds version 0, the version it read before the
	// first tab saved.
	_, err = repo.ConfigSet(ctx, repository.SetConfigParams{
		GuildID: "g1", BroadcasterID: 42, ExpectedVersion: 0,
		Config: ddiscord.Config{LogChannelID: "789"},
	})
	assert.ErrorIs(t, err, repository.ErrVersionConflict)

	cfg, version, _, err := repo.ConfigGet(ctx, "g1")
	require.NoError(t, err)
	assert.Equal(t, 1, version, "the losing write must not bump the version")
	assert.Empty(t, cfg.LogChannelID, "the losing write must not land")
}

func TestConfigSetRefusesACreateThatExpectedAVersion(t *testing.T) {
	repo, ctx := newStore(t, "configcreateconflict")
	bindOne(t, repo, ctx, "g1", 42)

	_, err := repo.ConfigSet(ctx, repository.SetConfigParams{
		GuildID: "g1", BroadcasterID: 42, ExpectedVersion: 3,
	})
	assert.ErrorIs(t, err, repository.ErrVersionConflict)
}

func TestConfigSetRefusesAGuildTheCallerDoesNotOwn(t *testing.T) {
	repo, ctx := newStore(t, "confignotbound")
	bindOne(t, repo, ctx, "g1", 42)

	_, err := repo.ConfigSet(ctx, repository.SetConfigParams{GuildID: "g1", BroadcasterID: 43})
	assert.ErrorIs(t, err, repository.ErrNotBound)

	// An unbound guild is the same refusal, so a caller cannot probe which
	// guild ids exist.
	_, err = repo.ConfigSet(ctx, repository.SetConfigParams{GuildID: "g9", BroadcasterID: 43})
	assert.ErrorIs(t, err, repository.ErrNotBound)
}

func TestConfigSetRejectsEmptyInput(t *testing.T) {
	repo, ctx := newStore(t, "configinvalid")

	_, err := repo.ConfigSet(ctx, repository.SetConfigParams{BroadcasterID: 42})
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
	_, err = repo.ConfigSet(ctx, repository.SetConfigParams{GuildID: "g1"})
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
	_, _, _, err = repo.ConfigGet(ctx, "")
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
}

func TestConfigIsPerGuild(t *testing.T) {
	repo, ctx := newStore(t, "configperguild")
	bindOne(t, repo, ctx, "g1", 42)
	bindOne(t, repo, ctx, "g2", 42)

	_, err := repo.ConfigSet(ctx, repository.SetConfigParams{
		GuildID: "g1", BroadcasterID: 42, Config: ddiscord.Config{LiveChannelID: "111"},
	})
	require.NoError(t, err)
	_, err = repo.ConfigSet(ctx, repository.SetConfigParams{
		GuildID: "g2", BroadcasterID: 42, Config: ddiscord.Config{LiveChannelID: "222"},
	})
	require.NoError(t, err)

	one, _, _, err := repo.ConfigGet(ctx, "g1")
	require.NoError(t, err)
	two, _, _, err := repo.ConfigGet(ctx, "g2")
	require.NoError(t, err)
	assert.Equal(t, "111", one.LiveChannelID)
	assert.Equal(t, "222", two.LiveChannelID)
}
