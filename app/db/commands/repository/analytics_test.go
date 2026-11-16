// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"fmt"
	"testing"

	"ItsBagelBot/app/db/commands/ent"
	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/internal/analytics"
	"ItsBagelBot/internal/analytics/analyticstest"
	catalog "ItsBagelBot/internal/domain/analytics"

	"github.com/stretchr/testify/require"
)

func newRecorder(t *testing.T) (*analytics.Recorder, func(context.Context) map[string]int64) {
	t.Helper()
	capture := analyticstest.New(t)
	read := func(ctx context.Context) map[string]int64 {
		return capture.Read(catalog.LevelKey)
	}
	return capture.Recorder, read
}

func TestCommandWritesShiftLevelsWithoutContent(t *testing.T) {
	_, _, repo := setup(t)
	rec, read := newRecorder(t)
	repo.SetAnalytics(rec)
	ctx := context.Background()

	hug := spec("!hug", "hugs", false, 0)
	hug.Aliases = []string{"cuddle"}
	so := spec("!so", "go follow", true, 30)
	so.Perm = "mod"
	old := spec("!old", "old", false, 0)
	old.IsActive = false
	old.BumpCounter = "deaths"

	require.NoError(t, repo.Upsert(1, hug))
	require.NoError(t, repo.Upsert(1, so))
	require.NoError(t, repo.Upsert(2, old))
	repo.Close(ctx)
	require.NoError(t, repo.Delete(ctx, 1, "!so"))

	got := read(ctx)
	require.Equal(t, map[string]int64{
		"custom.total|active":   1,
		"custom.total|inactive": 1,
		"custom.perm|everyone":  2,
		"custom.flags|aliased":  1,
		"custom.flags|counter":  1,
		"custom.channels":       2,
	}, got)
	for field := range got {
		require.NotContains(t, field, "hug")
		require.NotContains(t, field, "old")
	}
}

func TestFetchKeyWritesShiftConnectedChannels(t *testing.T) {
	_, _, repo := fetchSetup(t)
	rec, read := newRecorder(t)
	repo.SetAnalytics(rec)
	ctx := context.Background()

	for _, key := range []repository.KeyEntry{{Label: "a", Value: "secret-1111"}, {Label: "b", Value: "secret-2222"}} {
		_, err := repo.SetKey(ctx, 1, key)
		require.NoError(t, err)
	}
	_, err := repo.SetKey(ctx, 2, repository.KeyEntry{Label: "a", Value: "secret-3333"})
	require.NoError(t, err)
	require.NoError(t, repo.DeleteKey(ctx, 2, "a"))

	require.Equal(t, map[string]int64{"integrations.connected|fetch_key": 1}, read(ctx))
}

func TestAnalyticsSeedTalliesMixedRows(t *testing.T) {
	client, _, repo := setup(t)
	_, _, fetches := fetchSetup(t)
	ctx := context.Background()

	hug := spec("!hug", "hugs", false, 0)
	hug.Aliases = []string{"cuddle"}
	so := spec("!so", "go follow", true, 30)
	so.Perm = "mod"
	old := spec("!old", "old", false, 0)
	old.IsActive = false
	old.BumpCounter = "deaths"
	require.NoError(t, repo.Upsert(1, hug))
	require.NoError(t, repo.Upsert(1, so))
	require.NoError(t, repo.Upsert(2, old))
	require.NoError(t, repo.Upsert(3, spec("!hi", "hi", false, 0)))
	repo.Close(ctx)
	require.Equal(t, 4, client.Commands.Query().CountX(ctx))

	for _, key := range []repository.KeyEntry{{Label: "a", Value: "secret-1111"}, {Label: "b", Value: "secret-2222"}} {
		_, err := fetches.SetKey(ctx, 1, key)
		require.NoError(t, err)
	}
	_, err := fetches.SetKey(ctx, 3, repository.KeyEntry{Label: "a", Value: "secret-3333"})
	require.NoError(t, err)

	got, err := repo.AnalyticsSeed(ctx)
	require.NoError(t, err)
	require.Equal(t, analytics.Tally{
		"custom.total|active":      3,
		"custom.total|inactive":    1,
		"custom.perm|everyone":     3,
		"custom.perm|mod":          1,
		"custom.flags|online_only": 1,
		"custom.flags|aliased":     1,
		"custom.flags|cooldown":    1,
		"custom.flags|counter":     1,
		"custom.channels":          3,
	}, got)

	keys, err := fetches.AnalyticsSeed(ctx)
	require.NoError(t, err)
	require.Equal(t, analytics.Tally{"integrations.connected|fetch_key": 2}, keys)

	all, err := repository.SeedAll(repo, fetches)(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(2), all["integrations.connected|fetch_key"])
	require.Equal(t, int64(3), all["custom.channels"])
}

func TestAnalyticsSeedPagesPastOneThousandRows(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()

	const users, perUser = 25, 100
	var creates []*ent.CommandsCreate
	for u := uint64(1); u <= users; u++ {
		for i := 0; i < perUser; i++ {
			creates = append(creates, client.Commands.Create().
				SetUserID(u).SetName(fmt.Sprintf("!c%03d", i)).SetResponse("r").SetIsActive(i%2 == 0))
		}
	}
	for start := 0; start < len(creates); start += 100 {
		client.Commands.CreateBulk(creates[start : start+100]...).ExecX(ctx)
	}

	got, err := repo.AnalyticsSeed(ctx)
	require.NoError(t, err)
	require.Equal(t, analytics.Tally{
		"custom.total|active":   users * perUser / 2,
		"custom.total|inactive": users * perUser / 2,
		"custom.perm|everyone":  users * perUser,
		"custom.channels":       users,
	}, got)
}
