// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"math"
	"testing"

	"ItsBagelBot/internal/domain/event/data"

	"github.com/stretchr/testify/require"
)

func TestRecordUseReplayAndRangeRollback(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	defer repo.Close(ctx)
	row := client.Commands.Create().SetUserID(1001).SetName("hello").SetResponse("hello").SaveX(ctx)
	require.NoError(t, repo.RecordUse(ctx, "batch-1", data.CommandUsedDTO{UserID: 1001, Name: "!hello", Count: 3}))
	require.NoError(t, repo.RecordUse(ctx, "batch-1", data.CommandUsedDTO{UserID: 1001, Name: "hello", Count: 3}))
	require.Equal(t, int64(3), client.Commands.GetX(ctx, row.ID).Uses)
	require.Error(t, repo.RecordUse(ctx, "batch-1", data.CommandUsedDTO{UserID: 1001, Name: "hello", Count: 4}))
	require.Equal(t, int64(3), client.Commands.GetX(ctx, row.ID).Uses)
	client.Commands.UpdateOneID(row.ID).SetUses(int64(math.MaxInt64) - 1).SaveX(ctx)
	require.Error(t, repo.RecordUse(ctx, "overflow", data.CommandUsedDTO{UserID: 1001, Name: "hello", Count: 2}))
	require.Equal(t, int64(math.MaxInt64)-1, client.Commands.GetX(ctx, row.ID).Uses)
	_, err := client.CommandUseBatch.Get(ctx, "overflow")
	require.Error(t, err)
	// A failed transaction did not consume its identity, so a valid retry lands.
	require.NoError(t, repo.RecordUse(ctx, "overflow", data.CommandUsedDTO{UserID: 1001, Name: "hello", Count: 1}))
	require.Equal(t, int64(math.MaxInt64), client.Commands.GetX(ctx, row.ID).Uses)
	require.Error(t, repo.RecordUse(ctx, "too-big", data.CommandUsedDTO{UserID: 1001, Name: "hello", Count: -1}))
	require.Error(t, client.Commands.UpdateOneID(row.ID).SetUses(-1).Exec(ctx))
}

func TestRecordUseMissingCommandDoesNotResurrectOnReplay(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	defer repo.Close(ctx)
	require.NoError(t, repo.RecordUse(ctx, "missing", data.CommandUsedDTO{UserID: 1001, Name: "deleted", Count: 3}))
	row := client.Commands.Create().SetUserID(1001).SetName("deleted").SetResponse("new").SaveX(ctx)
	require.NoError(t, repo.RecordUse(ctx, "missing", data.CommandUsedDTO{UserID: 1001, Name: "deleted", Count: 3}))
	require.Zero(t, client.Commands.GetX(ctx, row.ID).Uses)
}

func TestRecordUsePreservesProducerBatchIncrements(t *testing.T) {
	client, _, repo := setup(t)
	ctx := context.Background()
	defer repo.Close(ctx)
	for _, name := range []string{"a", "b", "c"} {
		client.Commands.Create().SetUserID(1001).SetName(name).SetResponse("r").SetUses(10).SaveX(ctx)
	}

	for _, pending := range []struct {
		name  string
		count int64
	}{{"a", 1}, {"b", 1}, {"c", 3}, {"deleted", 1}} {
		require.NoError(t, repo.RecordUse(ctx, "batch-"+pending.name, data.CommandUsedDTO{UserID: 1001, Name: pending.name, Count: pending.count}))
	}

	got := map[string]int64{}
	for _, row := range client.Commands.Query().AllX(ctx) {
		got[row.Name] = row.Uses
	}
	require.Equal(t, map[string]int64{"a": 11, "b": 11, "c": 13}, got)
}
