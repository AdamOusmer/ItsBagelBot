// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/discord/repository"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEraseBroadcasterDeletesOnlyThatAccountsGuilds(t *testing.T) {
	f := newConfigFixture(t, "erasebroadcaster")
	for _, b := range []struct {
		guild       string
		broadcaster uint64
	}{{"g1", 42}, {"g2", 42}, {"g3", 43}} {
		f.bind(b.guild, b.broadcaster)
		_, err := f.repo.ConfigSet(f.ctx, repository.SetConfigParams{GuildID: b.guild, BroadcasterID: b.broadcaster, Config: ddiscord.Config{}})
		require.NoError(t, err)
	}

	erased, err := f.repo.EraseBroadcaster(f.ctx, 42, time.Now())
	require.NoError(t, err)
	assert.Equal(t, repository.EraseResult{Bindings: 2, Configs: 2}, erased)

	for _, guild := range []string{"g1", "g2"} {
		_, found, err := f.repo.BindingGet(f.ctx, guild)
		require.NoError(t, err)
		assert.False(t, found, guild)
		_, _, found, err = f.repo.ConfigGet(f.ctx, guild)
		require.NoError(t, err)
		assert.False(t, found, guild)
	}
	owner, found, err := f.repo.BindingGet(f.ctx, "g3")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, uint64(43), owner)

	again, err := f.repo.EraseBroadcaster(f.ctx, 42, time.Now())
	require.NoError(t, err)
	assert.Zero(t, again, "a retried erasure is a no-op")
}

func TestEraseBroadcasterKeepsRowsWrittenAfterDeletion(t *testing.T) {
	f := newConfigFixture(t, "erasereregistered")
	f.bind("g1", 42)
	_, err := f.repo.ConfigSet(f.ctx, repository.SetConfigParams{GuildID: "g1", BroadcasterID: 42, Config: ddiscord.Config{}})
	require.NoError(t, err)

	erased, err := f.repo.EraseBroadcaster(f.ctx, 42, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Zero(t, erased)

	_, found, err := f.repo.BindingGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.True(t, found, "the re-registered account's binding survives a late erasure")
	_, _, found, err = f.repo.ConfigGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.True(t, found)
}

func TestEraseBroadcasterRejectsMissingIdentity(t *testing.T) {
	repo, ctx := newStore(t, "eraseinvalid")

	_, err := repo.EraseBroadcaster(ctx, 0, time.Now())
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
	_, err = repo.EraseBroadcaster(ctx, 42, time.Time{})
	assert.ErrorIs(t, err, repository.ErrInvalidInput, "without a deletion time the re-registration fence is open")
}
