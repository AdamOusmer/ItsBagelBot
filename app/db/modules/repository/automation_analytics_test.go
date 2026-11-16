// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/db/modules/ent"
	"ItsBagelBot/app/db/modules/repository"
	"ItsBagelBot/internal/analytics/analyticstest"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/testdb"

	"github.com/stretchr/testify/require"
)

func TestRecipeAnalyticsTracksCommittedTriggerAndState(t *testing.T) {
	_, _, repo := setupAutomations(t, "recipeanalytics")
	capture := analyticstest.New(t)
	repo.SetAnalytics(capture.Recorder)
	ctx := t.Context()
	created, err := repo.Upsert(ctx, 1, repository.AutomationInput{Name: "one", Enabled: true, Definition: commandDef("!one")})
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"recipes.total|command|on": 1}, capture.Read(catalog.LevelKey))
	in := repository.AutomationInput{ID: created.ID, Name: "one", Definition: []byte(`{"version":1,"trigger":{"type":"manual","params":{}},"actions":[{"type":"chat","params":{"text":"hi"}}]}`)}
	for range 2 {
		_, err = repo.Upsert(ctx, 1, in)
		require.NoError(t, err)
	}
	require.Equal(t, map[string]int64{"recipes.total|manual|off": 1}, capture.Read(catalog.LevelKey))
	stale := int64(1)
	in.ExpectedRevision, in.Enabled = &stale, true
	_, err = repo.Upsert(ctx, 1, in)
	require.ErrorIs(t, err, repository.ErrRevisionConflict)
	in.ExpectedRevision = nil
	in.Definition = []byte(`{}`)
	_, err = repo.Upsert(ctx, 1, in)
	require.Error(t, err)
	require.Equal(t, map[string]int64{"recipes.total|manual|off": 1}, capture.Read(catalog.LevelKey))
	for _, user := range []uint64{2, 1, 1} {
		_, err := repo.Delete(ctx, user, created.ID)
		require.NoError(t, err)
	}
	require.Empty(t, capture.Read(catalog.LevelKey))
}

func TestRecipeAnalyticsIgnoresRolledBackCommit(t *testing.T) {
	client, _, repo := setupAutomations(t, "recipeanalyticsrollback")
	capture := analyticstest.New(t)
	repo.SetAnalytics(capture.Recorder)
	rejectTransactionCommit(t, client)
	_, err := repo.Upsert(t.Context(), 1, repository.AutomationInput{Name: "one", Enabled: true, Definition: commandDef("!one")})
	require.ErrorIs(t, err, errors.ErrUnsupported)
	require.Empty(t, capture.Read(catalog.LevelKey))
	require.Zero(t, client.Automation.Query().CountX(t.Context()))
}

func TestRecipeToggleAnalyticsTracksOnlyCommittedChanges(t *testing.T) {
	_, _, repo := setupAutomations(t, "recipetoggleanalytics")
	capture := analyticstest.New(t)
	repo.SetAnalytics(capture.Recorder)
	ctx := t.Context()
	created, err := repo.Upsert(ctx, 1, repository.AutomationInput{Name: "one", Enabled: true, Definition: commandDef("!one")})
	require.NoError(t, err)
	stale := created.Revision
	steps := []struct {
		name    string
		userID  uint64
		change  repository.EnabledChange
		wantErr error
		state   string
	}{
		{"disable", 1, repository.EnabledChange{ID: created.ID}, nil, "off"},
		{"repeat disable", 1, repository.EnabledChange{ID: created.ID}, nil, "off"},
		{"enable", 1, repository.EnabledChange{ID: created.ID, Enabled: true}, nil, "on"},
		{"repeat enable", 1, repository.EnabledChange{ID: created.ID, Enabled: true}, nil, "on"},
		{"stale revision", 1, repository.EnabledChange{ID: created.ID, ExpectedRevision: &stale}, repository.ErrRevisionConflict, "on"},
		{"wrong owner", 2, repository.EnabledChange{ID: created.ID}, repository.ErrAutomationNotFound, "on"},
		{"missing", 1, repository.EnabledChange{ID: "missing"}, repository.ErrAutomationNotFound, "on"},
	}
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			_, err := repo.SetEnabled(ctx, step.userID, step.change)
			require.ErrorIs(t, err, step.wantErr)
			require.Equal(t, map[string]int64{"recipes.total|command|" + step.state: 1}, capture.Read(catalog.LevelKey))
		})
	}
}

func TestRecipeToggleAnalyticsIgnoresFailedWrites(t *testing.T) {
	cases := []struct {
		name     string
		writeErr error
		wantErr  error
	}{
		{"revision race", nil, repository.ErrRevisionConflict},
		{"write error", errors.ErrUnsupported, errors.ErrUnsupported},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			client, _, repo := setupAutomations(t, testdb.Name(t.Name()))
			capture := analyticstest.New(t)
			repo.SetAnalytics(capture.Recorder)
			created, err := repo.Upsert(t.Context(), 1, repository.AutomationInput{Name: "one", Enabled: true, Definition: commandDef("!one")})
			require.NoError(t, err)
			client.Use(func(ent.Mutator) ent.Mutator {
				return ent.MutateFunc(func(context.Context, ent.Mutation) (ent.Value, error) { return 0, tt.writeErr })
			})
			_, err = repo.SetEnabled(t.Context(), 1, repository.EnabledChange{ID: created.ID})
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, map[string]int64{"recipes.total|command|on": 1}, capture.Read(catalog.LevelKey))
		})
	}
}

func rejectTransactionCommit(t *testing.T, client *ent.Client) {
	t.Helper()
	client.Use(func(next ent.Mutator) ent.Mutator {
		return ent.MutateFunc(func(ctx context.Context, mutation ent.Mutation) (ent.Value, error) {
			tx, err := mutation.(interface{ Tx() (*ent.Tx, error) }).Tx()
			require.NoError(t, err)
			tx.OnCommit(func(ent.Committer) ent.Committer {
				return ent.CommitFunc(func(context.Context, *ent.Tx) error {
					require.NoError(t, tx.Rollback())
					return errors.ErrUnsupported
				})
			})
			return next.Mutate(ctx, mutation)
		})
	})
}

func TestRecipeAnalyticsCoversConversionAndAccountCleanup(t *testing.T) {
	rig := newAlertRig(t, "recipeanalyticsconversion")
	capture := analyticstest.New(t)
	rig.modules.SetAnalytics(capture.Recorder)
	rig.repo.SetAnalytics(capture.Recorder)
	ctx := t.Context()
	in := repository.AutomationInput{Name: "Flow", Definition: []byte(followFlow)}
	row, err := rig.repo.UpgradeAlert(ctx, 1001, in, repository.AlertUpgrade{EnableKey: "followEnabled"})
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"recipes.total|event|off": 1}, capture.Read(catalog.LevelKey))
	stale := int64(99)
	err = rig.repo.DowngradeToAlert(ctx, 1001, row.ID, repository.AlertDowngrade{EnableKey: "followEnabled", ExpectedRevision: &stale})
	require.ErrorIs(t, err, repository.ErrRevisionConflict)
	require.Equal(t, map[string]int64{"recipes.total|event|off": 1}, capture.Read(catalog.LevelKey))
	err = rig.repo.DowngradeToAlert(ctx, 1001, row.ID, repository.AlertDowngrade{EnableKey: "followEnabled"})
	require.NoError(t, err)
	require.Equal(t, map[string]int64{"modules.rows|alerts|off": 1, "modules.breadth|0": 1}, capture.Read(catalog.LevelKey))
	in.Enabled = true
	_, err = rig.repo.UpgradeAlert(ctx, 1001, in, repository.AlertUpgrade{EnableKey: "followEnabled"})
	require.NoError(t, err)
	require.Equal(t, map[string]int64{
		"modules.rows|alerts|off": 1, "modules.breadth|0": 1, "recipes.total|event|on": 1,
	}, capture.Read(catalog.LevelKey))
	rig.modules.SetAccountInstanceResolver(func(context.Context, uint64) (int64, error) { return 100, nil })
	require.NoError(t, rig.modules.DeleteAccount(ctx, 1001, 100))
	require.Empty(t, capture.Read(catalog.LevelKey))
	require.Equal(t, map[string]int64{"module.toggles|alerts|off": 1}, capture.Counters())
}
