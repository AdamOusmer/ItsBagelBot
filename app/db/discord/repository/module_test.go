// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/discord/repository"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fullLegacyConfig(guildID string) ddiscord.Config {
	cfg := ddiscord.Config{}
	v := reflect.ValueOf(&cfg).Elem()
	for i := range v.NumField() {
		tag, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("json"), ",")
		v.Field(i).SetString("v-" + tag)
	}
	cfg.GuildID = guildID
	for _, spec := range ddiscord.Modules {
		if spec.EnabledKey == "" {
			continue
		}
		for i := range v.NumField() {
			if tag, _, _ := strings.Cut(v.Type().Field(i).Tag.Get("json"), ","); tag == spec.EnabledKey {
				v.Field(i).SetString("off")
			}
		}
	}
	return cfg
}

func (f configFixture) module(guildID, name string) (ddiscord.ModuleRow, bool) {
	f.t.Helper()
	rows, _, err := f.repo.ModulesGet(f.ctx, guildID)
	require.NoError(f.t, err)
	for _, row := range rows {
		if row.Name == name {
			return row, true
		}
	}
	return ddiscord.ModuleRow{}, false
}

func TestModulesGetMissingGuildIsNotAnError(t *testing.T) {
	f := newConfigFixture(t, "modulesmissing")

	rows, found, err := f.repo.ModulesGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.False(t, found)
	assert.Empty(t, rows)

	_, _, err = f.repo.ModulesGet(f.ctx, "")
	assert.ErrorIs(t, err, repository.ErrInvalidInput)
}

func TestModulesGetBackfillsEveryLegacyFieldAndIsIdempotent(t *testing.T) {
	f := newConfigFixture(t, "modulesbackfill")
	legacy := fullLegacyConfig("g1")
	require.NoError(t, f.repo.PutLegacyConfig(f.ctx, "g1", 42, legacy))

	rows, found, err := f.repo.ModulesGet(f.ctx, "g1")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, legacy, ddiscord.JoinModules("g1", rows), "backfill must not lose a legacy field")
	for _, row := range rows {
		assert.Equal(t, 1, row.Version, row.Name)
	}

	again, found, err := f.repo.ModulesGet(f.ctx, "g1")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, rows, again, "a second read must not duplicate or bump rows")

	stored, version, err := f.repo.LegacyConfig(f.ctx, "g1")
	require.NoError(t, err)
	assert.Equal(t, legacy, stored, "the legacy row is never rewritten by a read")
	assert.Equal(t, 1, version)
}

func TestModuleSetCreatesThenUpdatesWithVersions(t *testing.T) {
	f := newConfigFixture(t, "modulescreate")
	f.bind("g1", 42)

	version, err := f.repo.ModuleSet(f.ctx, repository.ModuleSetParams{
		GuildID: "g1", BroadcasterID: 42, Name: ddiscord.ModuleClips, Enabled: true,
		Settings: codec.RawMessage(`{"clipsChannelId":"123"}`),
	})
	require.NoError(t, err)
	assert.Equal(t, 1, version)

	version, err = f.repo.ModuleSet(f.ctx, repository.ModuleSetParams{
		GuildID: "g1", BroadcasterID: 42, Name: ddiscord.ModuleClips, Enabled: false,
		Settings: codec.RawMessage(`{"clipsChannelId":"456"}`), ExpectedVersion: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, version)

	row, ok := f.module("g1", ddiscord.ModuleClips)
	require.True(t, ok)
	assert.False(t, row.Enabled)
	assert.Equal(t, 2, row.Version)
	assert.JSONEq(t, `{"clipsChannelId":"456"}`, string(row.Settings))
}

func TestModuleSetRefusesStaleAndMisdeclaredVersions(t *testing.T) {
	f := newConfigFixture(t, "modulesconflict")
	f.bind("g1", 42)
	set := func(expected int) error {
		_, err := f.repo.ModuleSet(f.ctx, repository.ModuleSetParams{
			GuildID: "g1", BroadcasterID: 42, Name: ddiscord.ModuleLive, Enabled: true, ExpectedVersion: expected,
		})
		return err
	}

	require.NoError(t, set(0))
	assert.ErrorIs(t, set(0), repository.ErrVersionConflict, "expected 0 on an existing row")
	assert.ErrorIs(t, set(5), repository.ErrVersionConflict, "stale or invented version")
	require.NoError(t, set(1))
	assert.ErrorIs(t, set(1), repository.ErrVersionConflict, "the version already moved on")

	row, _ := f.module("g1", ddiscord.ModuleLive)
	assert.Equal(t, 2, row.Version, "losing writes must not bump the version")
}

func TestModuleSetRefusesAnUpdateForARowThatDoesNotExist(t *testing.T) {
	f := newConfigFixture(t, "modulesmissingupdate")
	f.bind("g1", 42)

	_, err := f.repo.ModuleSet(f.ctx, repository.ModuleSetParams{
		GuildID: "g1", BroadcasterID: 42, Name: ddiscord.ModuleLive, ExpectedVersion: 3,
	})
	assert.ErrorIs(t, err, repository.ErrVersionConflict)
}

func TestModuleSetRefusesAGuildTheCallerDoesNotOwn(t *testing.T) {
	f := newConfigFixture(t, "modulesnotbound")
	f.bind("g1", 42)

	_, err := f.repo.ModuleSet(f.ctx, repository.ModuleSetParams{GuildID: "g1", BroadcasterID: 43, Name: ddiscord.ModuleLive})
	assert.ErrorIs(t, err, repository.ErrNotBound)
	_, err = f.repo.ModuleSet(f.ctx, repository.ModuleSetParams{GuildID: "g9", BroadcasterID: 42, Name: ddiscord.ModuleLive})
	assert.ErrorIs(t, err, repository.ErrNotBound)

	_, found, err := f.repo.ModulesGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.False(t, found, "a refused write must leave nothing behind")
}

func TestModuleSetRejectsBadInput(t *testing.T) {
	f := newConfigFixture(t, "modulesinvalid")

	for _, p := range []repository.ModuleSetParams{
		{BroadcasterID: 42, Name: ddiscord.ModuleLive},
		{GuildID: "g1", Name: ddiscord.ModuleLive},
		{GuildID: "g1", BroadcasterID: 42, Name: "nope"},
		{GuildID: "g1", BroadcasterID: 42, Name: ddiscord.ModuleIdentity},
	} {
		_, err := f.repo.ModuleSet(f.ctx, p)
		assert.ErrorIs(t, err, repository.ErrInvalidInput, p.Name)
	}
}

func TestModuleSetKeepsCoreModulesEnabled(t *testing.T) {
	f := newConfigFixture(t, "modulescore")
	f.bind("g1", 42)

	_, err := f.repo.ModuleSet(f.ctx, repository.ModuleSetParams{
		GuildID: "g1", BroadcasterID: 42, Name: ddiscord.ModuleRoles, Enabled: false,
	})
	require.NoError(t, err)

	row, ok := f.module("g1", ddiscord.ModuleRoles)
	require.True(t, ok)
	assert.True(t, row.Enabled)
}

func TestModuleSetBackfillsLegacyBeforeWriting(t *testing.T) {
	f := newConfigFixture(t, "modulesbackfillwrite")
	f.bind("g1", 42)
	legacy := fullLegacyConfig("g1")
	require.NoError(t, f.repo.PutLegacyConfig(f.ctx, "g1", 42, legacy))

	version, err := f.repo.ModuleSet(f.ctx, repository.ModuleSetParams{
		GuildID: "g1", BroadcasterID: 42, Name: ddiscord.ModuleClips, Enabled: true,
		Settings: codec.RawMessage(`{"clipsChannelId":"999"}`), ExpectedVersion: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, 2, version)

	want := legacy
	want.ClipsChannelID = "999"
	want.ClipsEnabled = "on"
	rows, _, err := f.repo.ModulesGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.Equal(t, want, ddiscord.JoinModules("g1", rows), "other modules keep their legacy values")
}

func TestModuleSetMirrorsIntoTheLegacyRow(t *testing.T) {
	f := newConfigFixture(t, "modulesmirror")
	f.bind("g1", 42)

	_, err := f.repo.ModuleSet(f.ctx, repository.ModuleSetParams{
		GuildID: "g1", BroadcasterID: 42, Name: ddiscord.ModuleLive, Enabled: false,
		Settings: codec.RawMessage(`{"liveChannelId":"111"}`),
	})
	require.NoError(t, err)

	cfg, version, err := f.repo.LegacyConfig(f.ctx, "g1")
	require.NoError(t, err)
	assert.Equal(t, 1, version, "the legacy row is created at version 1")
	assert.Equal(t, "111", cfg.LiveChannelID)
	assert.Equal(t, "off", cfg.LiveEnabled)

	_, err = f.repo.ModuleSet(f.ctx, repository.ModuleSetParams{
		GuildID: "g1", BroadcasterID: 42, Name: ddiscord.ModuleLive, Enabled: true,
		Settings: codec.RawMessage(`{"liveChannelId":"222"}`), ExpectedVersion: 1,
	})
	require.NoError(t, err)

	cfg, version, err = f.repo.LegacyConfig(f.ctx, "g1")
	require.NoError(t, err)
	assert.Equal(t, 2, version, "every module write bumps the legacy version")
	assert.Equal(t, "222", cfg.LiveChannelID)
	assert.Equal(t, "on", cfg.LiveEnabled)

	legacyView, legacyVersion, found, err := f.repo.ConfigGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 2, legacyVersion)
	assert.Equal(t, cfg, legacyView)
}

func TestLegacyConfigSetUpdatesModuleRows(t *testing.T) {
	f := newConfigFixture(t, "modulesfromlegacy")
	f.bind("g1", 42)
	first := ddiscord.Config{GuildID: "g1", LiveChannelID: "111", LiveEnabled: "off", SubscribersEnabled: "on"}

	_, err := f.repo.ConfigSet(f.ctx, repository.SetConfigParams{GuildID: "g1", BroadcasterID: 42, Config: first})
	require.NoError(t, err)

	live, ok := f.module("g1", ddiscord.ModuleLive)
	require.True(t, ok)
	assert.False(t, live.Enabled)
	assert.Equal(t, 1, live.Version)
	subs, _ := f.module("g1", ddiscord.ModuleSubscribers)
	assert.True(t, subs.Enabled)

	second := ddiscord.Config{GuildID: "g1", LiveChannelID: "222", LiveEnabled: "on"}
	_, err = f.repo.ConfigSet(f.ctx, repository.SetConfigParams{GuildID: "g1", BroadcasterID: 42, Config: second, ExpectedVersion: 1})
	require.NoError(t, err)

	live, _ = f.module("g1", ddiscord.ModuleLive)
	assert.True(t, live.Enabled)
	assert.Equal(t, 2, live.Version, "a legacy save bumps each module version")
	assert.JSONEq(t, `{"liveChannelId":"222","categoryAllow":"","categoryDeny":"","twitchLogin":""}`, string(live.Settings))
	subs, _ = f.module("g1", ddiscord.ModuleSubscribers)
	assert.False(t, subs.Enabled)

	got, version, _, err := f.repo.ConfigGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.Equal(t, 2, version)
	assert.Equal(t, "222", got.LiveChannelID)
}

func TestLegacyConfigSetOverwritesAnUnmigratedGuildsRows(t *testing.T) {
	f := newConfigFixture(t, "modulesfromunmigrated")
	f.bind("g1", 42)
	require.NoError(t, f.repo.PutLegacyConfig(f.ctx, "g1", 42, fullLegacyConfig("g1")))

	next := ddiscord.Config{GuildID: "g1", ClipsChannelID: "777"}
	_, err := f.repo.ConfigSet(f.ctx, repository.SetConfigParams{GuildID: "g1", BroadcasterID: 42, Config: next, ExpectedVersion: 1})
	require.NoError(t, err)

	rows, found, err := f.repo.ModulesGet(f.ctx, "g1")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, ddiscord.JoinModules("g1", ddiscord.SplitConfig(next)), ddiscord.JoinModules("g1", rows))
}

func TestModuleSetFirstEverWriteHasOneWinner(t *testing.T) {
	f := newConcurrentConfigFixture(t, "modulesrace")
	f.bind("g1", 42)

	const callers = 4
	var wg sync.WaitGroup
	errs := make([]error, callers)
	wg.Add(callers)
	for i := range callers {
		go func() {
			defer wg.Done()
			_, errs[i] = f.repo.ModuleSet(f.ctx, repository.ModuleSetParams{
				GuildID: "g1", BroadcasterID: 42, Name: ddiscord.ModuleLive, Enabled: true,
				Settings: codec.RawMessage(`{"liveChannelId":"` + strconv.Itoa(i) + `"}`),
			})
		}()
	}
	wg.Wait()

	won := 0
	for i, err := range errs {
		if err == nil {
			won++
			continue
		}
		require.ErrorIs(t, err, repository.ErrVersionConflict, "writer %d", i)
	}
	assert.Equal(t, 1, won)
}

func TestEraseBroadcasterRemovesModuleRowsBehindTheSameGuard(t *testing.T) {
	f := newConfigFixture(t, "erasemodules")
	f.bind("g1", 42)
	_, err := f.repo.ModuleSet(f.ctx, repository.ModuleSetParams{GuildID: "g1", BroadcasterID: 42, Name: ddiscord.ModuleLive})
	require.NoError(t, err)

	erased, err := f.repo.EraseBroadcaster(f.ctx, 42, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.Zero(t, erased.Modules, "rows newer than the deletion survive")
	_, found, err := f.repo.ModulesGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.True(t, found)

	erased, err = f.repo.EraseBroadcaster(f.ctx, 42, time.Now())
	require.NoError(t, err)
	assert.Equal(t, 1, erased.Modules)
	_, found, err = f.repo.ModulesGet(f.ctx, "g1")
	require.NoError(t, err)
	assert.False(t, found, "erased guild must not be resurrected by a read")
}
