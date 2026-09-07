// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"ItsBagelBot/app/db/discord/repository"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// configFixture is one test's store together with the context its calls run
// under. The three travel as one value because every helper below needs all
// of them, and passed separately they are three of the five arguments on a
// two-line helper -- the argument list stops saying what the call does.
type configFixture struct {
	t    *testing.T
	repo *repository.Store
	ctx  context.Context
}

// newConfigFixture opens the store one test works against.
func newConfigFixture(t *testing.T, name string) configFixture {
	t.Helper()
	repo, ctx := newStore(t, name)
	return configFixture{t: t, repo: repo, ctx: ctx}
}

// newConcurrentConfigFixture is the same over the store the race tests need
// (see newConcurrentStore for why those two differ).
func newConcurrentConfigFixture(t *testing.T, name string) configFixture {
	t.Helper()
	repo, ctx := newConcurrentStore(t, name)
	return configFixture{t: t, repo: repo, ctx: ctx}
}

// bind binds one guild so the config paths under test have an owner.
func (f configFixture) bind(guildID string, broadcasterID uint64) {
	f.t.Helper()
	require.NoError(f.t, f.repo.BindingSet(f.ctx, repository.BindParams{GuildID: guildID, BroadcasterID: broadcasterID}))
}

func TestConfigGetMissingIsNotAnError(t *testing.T) {
	f := newConfigFixture(t, "configmissing")

	cfg, version, found, err := f.repo.ConfigGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Zero(t, version)
	assert.Equal(t, ddiscord.Config{}, cfg)
}

func TestConfigSetCreatesThenUpdates(t *testing.T) {
	f := newConfigFixture(t, "configcreate")
	f.bind("g1", 42)

	version, err := f.repo.ConfigSet(f.ctx, repository.SetConfigParams{
		GuildID: "g1", BroadcasterID: 42,
		Config: ddiscord.Config{GuildID: "g1", LiveChannelID: "123", LiveEnabled: "on"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, version)

	cfg, version, found, err := f.repo.ConfigGet(f.ctx, "g1")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, 1, version)
	assert.Equal(t, "123", cfg.LiveChannelID)
	assert.True(t, cfg.LiveOn())

	version, err = f.repo.ConfigSet(f.ctx, repository.SetConfigParams{
		GuildID: "g1", BroadcasterID: 42, ExpectedVersion: 1,
		Config: ddiscord.Config{GuildID: "g1", LiveChannelID: "456"},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, version)

	cfg, _, _, err = f.repo.ConfigGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.Equal(t, "456", cfg.LiveChannelID)
}

func TestConfigSetRefusesAStaleVersion(t *testing.T) {
	f := newConfigFixture(t, "configconflict")
	f.bind("g1", 42)

	_, err := f.repo.ConfigSet(f.ctx, repository.SetConfigParams{GuildID: "g1", BroadcasterID: 42})
	require.NoError(t, err)

	// The second tab still holds version 0, the version it read before the
	// first tab saved.
	_, err = f.repo.ConfigSet(f.ctx, repository.SetConfigParams{
		GuildID: "g1", BroadcasterID: 42, ExpectedVersion: 0,
		Config: ddiscord.Config{LogChannelID: "789"},
	})
	assert.ErrorIs(t, err, repository.ErrVersionConflict)

	cfg, version, _, err := f.repo.ConfigGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.Equal(t, 1, version, "the losing write must not bump the version")
	assert.Empty(t, cfg.LogChannelID, "the losing write must not land")
}

func TestConfigSetRefusesACreateThatExpectedAVersion(t *testing.T) {
	f := newConfigFixture(t, "configcreateconflict")
	f.bind("g1", 42)

	_, err := f.repo.ConfigSet(f.ctx, repository.SetConfigParams{
		GuildID: "g1", BroadcasterID: 42, ExpectedVersion: 3,
	})
	assert.ErrorIs(t, err, repository.ErrVersionConflict)
}

func TestConfigSetRefusesAGuildTheCallerDoesNotOwn(t *testing.T) {
	f := newConfigFixture(t, "confignotbound")
	f.bind("g1", 42)

	_, err := f.repo.ConfigSet(f.ctx, repository.SetConfigParams{GuildID: "g1", BroadcasterID: 43})
	assert.ErrorIs(t, err, repository.ErrNotBound)

	// An unbound guild is the same refusal, so a caller cannot probe which
	// guild ids exist.
	_, err = f.repo.ConfigSet(f.ctx, repository.SetConfigParams{GuildID: "g9", BroadcasterID: 43})
	assert.ErrorIs(t, err, repository.ErrNotBound)
}

func TestConfigSetRejectsEmptyInput(t *testing.T) {
	f := newConfigFixture(t, "configinvalid")

	_, err := f.repo.ConfigSet(f.ctx, repository.SetConfigParams{BroadcasterID: 42})
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
	_, err = f.repo.ConfigSet(f.ctx, repository.SetConfigParams{GuildID: "g1"})
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
	_, _, _, err = f.repo.ConfigGet(f.ctx, "")
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
}

func TestConfigIsPerGuild(t *testing.T) {
	f := newConfigFixture(t, "configperguild")
	f.bind("g1", 42)
	f.bind("g2", 42)

	_, err := f.repo.ConfigSet(f.ctx, repository.SetConfigParams{
		GuildID: "g1", BroadcasterID: 42, Config: ddiscord.Config{LiveChannelID: "111"},
	})
	require.NoError(t, err)
	_, err = f.repo.ConfigSet(f.ctx, repository.SetConfigParams{
		GuildID: "g2", BroadcasterID: 42, Config: ddiscord.Config{LiveChannelID: "222"},
	})
	require.NoError(t, err)

	one, _, _, err := f.repo.ConfigGet(f.ctx, "g1")
	require.NoError(t, err)
	two, _, _, err := f.repo.ConfigGet(f.ctx, "g2")
	require.NoError(t, err)
	assert.Equal(t, "111", one.LiveChannelID)
	assert.Equal(t, "222", two.LiveChannelID)
}

// writers runs callers concurrent ConfigSet calls that all claim expected and
// reports how many were accepted. Every refusal must be ErrVersionConflict: a
// losing writer is a conflict, never an internal error.
func (f configFixture) writers(expected int) int {
	f.t.Helper()
	const callers = 4
	var wg sync.WaitGroup
	errs := make([]error, callers)
	wg.Add(callers)
	for i := range callers {
		go func() {
			defer wg.Done()
			_, errs[i] = f.repo.ConfigSet(f.ctx, repository.SetConfigParams{
				GuildID: "g1", BroadcasterID: 42, ExpectedVersion: expected,
				Config: ddiscord.Config{GuildID: "g1", LiveChannelID: strconv.Itoa(i)},
			})
		}()
	}
	wg.Wait()

	won := 0
	for i := range callers {
		if errs[i] == nil {
			won++
			continue
		}
		require.ErrorIs(f.t, errs[i], repository.ErrVersionConflict, "writer %d", i)
	}
	return won
}

// TestConfigSetFirstEverWriteHasOneWinner is the insert race. A row lock takes
// nothing on a row that does not exist, so several first-ever saves can all
// reach the INSERT; the unique index on guild_id decides, and the losers must
// surface as version conflicts rather than as constraint errors.
func TestConfigSetFirstEverWriteHasOneWinner(t *testing.T) {
	f := newConcurrentConfigFixture(t, "configinsertrace")
	f.bind("g1", 42)

	assert.Equal(t, 1, f.writers(0), "exactly one first-ever write may be accepted")

	_, version, found, err := f.repo.ConfigGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 1, version)
}

// TestConfigSetUpdateHasOneWinner is the update race, and the reason the write
// is a conditional UPDATE rather than a read followed by UpdateOne: two tabs
// holding version 1 must not both save, because each holds a whole Config and
// the loser's blob would silently replace the winner's.
func TestConfigSetUpdateHasOneWinner(t *testing.T) {
	f := newConcurrentConfigFixture(t, "configupdaterace")
	f.bind("g1", 42)
	_, err := f.repo.ConfigSet(f.ctx, repository.SetConfigParams{
		GuildID: "g1", BroadcasterID: 42, Config: ddiscord.Config{GuildID: "g1"},
	})
	require.NoError(t, err)

	assert.Equal(t, 1, f.writers(1), "exactly one writer at version 1 may be accepted")

	_, version, _, err := f.repo.ConfigGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.Equal(t, 2, version)
}
