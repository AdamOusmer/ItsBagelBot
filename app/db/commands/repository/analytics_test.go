// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/commands/repository"
	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newRecorder(t *testing.T) (*analytics.Recorder, func(context.Context) map[string]int64) {
	t.Helper()
	client := valkeytest.Spawn(t)
	rec := analytics.New(client, zap.NewNop(), nil, analytics.WithoutTicker())
	read := func(ctx context.Context) map[string]int64 {
		rec.Close()
		raw, err := client.Do(ctx, client.B().Hgetall().Key(catalog.LevelKey).Build()).AsIntMap()
		require.NoError(t, err)
		out := map[string]int64{}
		for field, n := range raw {
			if n != 0 {
				out[field] = n
			}
		}
		return out
	}
	return rec, read
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
