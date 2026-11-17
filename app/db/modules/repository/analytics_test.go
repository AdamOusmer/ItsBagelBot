// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/analytics"
	"ItsBagelBot/internal/analytics/analyticstest"
	catalog "ItsBagelBot/internal/domain/analytics"

	"github.com/stretchr/testify/require"
)

func TestAnalyticsSeedTalliesStoredRows(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	row := func(user uint64, name string, enabled bool) *ent.ModulesCreate {
		return client.Modules.Create().SetUserID(user).SetName(name).SetIsEnabled(enabled).SetRevision(0).SetConfigs([]byte(`{}`))
	}
	client.Modules.CreateBulk(
		row(1, "quotes", true), row(1, "loyalty", true), row(1, "bogus", true),
		row(2, "accountage", true), row(2, "alerts", true), row(2, "automod", true),
		row(2, "clip", true), row(2, "codm", true), row(2, "counters", true),
		row(3, "quotes", false),
	).ExecX(ctx)
	client.SpotifyCredential.Create().SetUserID(1).SetTokenEnc([]byte("token")).ExecX(ctx)
	client.SpotifyCredential.Create().SetUserID(2).ExecX(ctx)
	client.GoveeCredential.Create().SetUserID(1).SetKeyEnc([]byte("key")).ExecX(ctx)
	client.GoveeCredential.Create().SetUserID(3).SetKeyEnc([]byte("key")).ExecX(ctx)

	got, err := repo.AnalyticsSeed(ctx)
	require.NoError(t, err)
	requireModuleDefaults(t, got)
	require.Equal(t, analytics.Tally{
		"modules.rows|quotes|on":         1,
		"modules.rows|loyalty|on":        1,
		"modules.rows|accountage|on":     1,
		"modules.rows|alerts|on":         1,
		"modules.rows|automod|on":        1,
		"modules.rows|clip|on":           1,
		"modules.rows|codm|on":           1,
		"modules.rows|counters|on":       1,
		"modules.rows|quotes|off":        1,
		"modules.breadth|2":              1,
		"modules.breadth|6":              1,
		"modules.breadth|0":              1,
		"integrations.connected|spotify": 1,
		"integrations.connected|govee":   2,
	}, got)
}

func TestModuleTogglesOnlyCountCommittedEnabledChanges(t *testing.T) {
	client, _, repo := setup(t)
	capture := analyticstest.New(t)
	repo.SetAnalytics(capture.Recorder)
	ctx := t.Context()
	for _, enabled := range []bool{false, false, true, true} {
		_, err := repo.SetNow(ctx, 1, "quotes", enabled, nil)
		require.NoError(t, err)
	}
	_, err := repo.Patch(ctx, 1, "quotes", true, nil, nil)
	require.NoError(t, err)
	stale := 0
	result, err := repo.Patch(ctx, 1, "quotes", false, nil, &stale)
	require.NoError(t, err)
	require.True(t, result.Conflict)
	row := client.Modules.Query().OnlyX(ctx)
	result, err = repo.PatchExisting(ctx, 1, "quotes", false, nil, row.Revision, row.ID+1)
	require.NoError(t, err)
	require.True(t, result.Conflict)
	_, err = repo.Patch(ctx, 1, "quotes", false, nil, nil)
	require.NoError(t, err)
	for _, enabled := range []bool{true, false, false, true} {
		_, err := repo.SetNow(ctx, 2, "alerts", enabled, nil)
		require.NoError(t, err)
	}
	_, err = repo.SetNow(ctx, 3, "unknown", true, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]int64{
		"module.toggles|quotes|on": 1, "module.toggles|quotes|off": 1,
		"module.toggles|alerts|off": 1, "module.toggles|alerts|on": 1,
	}, capture.Counters())
	seed, err := repo.AnalyticsSeed(ctx)
	require.NoError(t, err)
	requireModuleDefaults(t, seed)
	require.Equal(t, seed, analytics.Tally(readModuleSeed(t, repo)))
}

func TestModuleRolledBackCommitDoesNotRecordAnalytics(t *testing.T) {
	client, _, repo := setup(t)
	capture := analyticstest.New(t)
	repo.SetAnalytics(capture.Recorder)
	rejectTransactionCommit(t, client)
	_, err := repo.SetNow(t.Context(), 1, "quotes", true, nil)
	require.ErrorIs(t, err, errors.ErrUnsupported)
	require.Empty(t, capture.Counters())
	require.Empty(t, readModuleSeed(t, repo))
}

func TestRecipeSeedPagesAndRejectsUnknownTriggers(t *testing.T) {
	client, _, repo := setup(t)
	for i := range 1101 {
		trigger := "timer"
		if i == 1100 {
			trigger = "private-input"
		}
		name := fmt.Sprintf("recipe%04d", i)
		client.Automation.Create().SetID(name).SetName(name).SetNameKey(name).
			SetUserID(1).SetEnabled(i%2 == 0).SetTriggerType(trigger).SetDefinition(`{}`).ExecX(t.Context())
	}
	seed, err := repo.AnalyticsSeed(t.Context())
	require.NoError(t, err)
	requireModuleDefaults(t, seed)
	require.Equal(t, analytics.Tally{"recipes.total|timer|on": 550, "recipes.total|timer|off": 550}, seed)
}

func TestAnalyticsSeedPagesPastOneBatch(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	const users = 1100
	builders := make([]*ent.ModulesCreate, 0, users)
	for id := uint64(1); id <= users; id++ {
		builders = append(builders, client.Modules.Create().SetUserID(id).SetName("quotes").SetIsEnabled(id%2 == 0).SetRevision(0).SetConfigs([]byte(`{}`)))
	}
	client.Modules.CreateBulk(builders...).ExecX(ctx)

	got, err := repo.AnalyticsSeed(ctx)
	require.NoError(t, err)
	requireModuleDefaults(t, got)
	require.Equal(t, analytics.Tally{
		"modules.rows|quotes|on":  users / 2,
		"modules.rows|quotes|off": users / 2,
		"modules.breadth|1":       users / 2,
		"modules.breadth|0":       users / 2,
	}, got)
}

func requireModuleDefaults(t *testing.T, tally analytics.Tally) {
	t.Helper()
	for _, name := range catalog.ModuleNames() {
		field := catalog.Field(catalog.ModulesDefaults, name)
		want := map[bool]int64{true: 1}[catalog.ModuleEnabledByDefault(name)]
		require.Equal(t, want, tally[field], name)
		delete(tally, field)
	}
}

func TestSQLLevelsFollowCommittedWrites(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	capture := analyticstest.New(t)
	rec := capture.Recorder
	govee := repository.NewGoveeCreds(client, dbtest.NewPacker(t))
	repo.SetAnalytics(rec)

	_, err := repo.SetNow(ctx, 1, "quotes", true, nil)
	require.NoError(t, err)
	_, err = repo.Patch(ctx, 1, "loyalty", true, nil, nil)
	require.NoError(t, err)
	_, err = repo.SetNow(ctx, 2, "quotes", true, nil)
	require.NoError(t, err)
	_, err = repo.SetNow(ctx, 1, "quotes", false, nil)
	require.NoError(t, err)
	require.NoError(t, repo.DeleteAllForUser(ctx, 2))
	require.NoError(t, govee.SetKey(ctx, 1, "key"))
	require.NoError(t, govee.SetKey(ctx, 3, "key"))
	require.NoError(t, govee.ClearKey(ctx, 3))

	require.Equal(t, map[string]int64{
		"modules.rows|quotes|off":      1,
		"modules.rows|loyalty|on":      1,
		"modules.breadth|1":            1,
		"integrations.connected|govee": 1,
	}, readModuleSeed(t, repo))
}

func readModuleSeed(t *testing.T, repo *repository.Modules) map[string]int64 {
	t.Helper()
	seed, err := repo.AnalyticsSeed(t.Context())
	require.NoError(t, err)
	requireModuleDefaults(t, seed)
	return map[string]int64(seed)
}
