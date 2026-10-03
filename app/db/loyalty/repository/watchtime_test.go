// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package repository_test

import (
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/event/data"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWatchPostingDedupsChunkAndViewerAcrossChunks(t *testing.T) {
	repo, _ := newLoyaltyRepo(t)
	ctx := context.Background()
	require.NoError(t, repo.EnsureWatchSchema(ctx))
	a := watchAward(17, 77, 100)
	require.NoError(t, repo.ApplyWatchAward(ctx, a))
	require.NoError(t, repo.ApplyWatchAward(ctx, a))
	a.Chunk = 1
	require.NoError(t, repo.ApplyWatchAward(ctx, a))
	row, found, err := repo.BalanceGet(ctx, 17, 77)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 10, row.Points)
	require.EqualValues(t, 300, row.WatchSeconds)
	other := watchAward(18, 77, 100)
	require.NoError(t, repo.ApplyWatchAward(ctx, other))
	row, found, err = repo.BalanceGet(ctx, 18, 77)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 10, row.Points)
	a.Entries[0].Points = 11
	require.ErrorContains(t, repo.ApplyWatchAward(ctx, a), "payload mismatch")
}
func TestWatchPostingDeletionAndRecreationFence(t *testing.T) {
	repo, _ := newLoyaltyRepo(t)
	ctx := context.Background()
	require.NoError(t, repo.EnsureWatchSchema(ctx))
	require.NoError(t, repo.RestoreUser(ctx, 17, 100))
	a := watchAward(17, 77, 100)
	repo.RecordEarned(data.LoyaltyEarnedDTO{UserID: 17, Entries: a.Entries})
	require.NoError(t, repo.DeleteAccount(ctx, 17, 100))
	require.NoError(t, repo.ApplyWatchAward(ctx, a))
	repo.Flush(ctx)
	_, found, err := repo.BalanceGet(ctx, 17, 77)
	require.NoError(t, err)
	require.False(t, found)
	require.NoError(t, repo.RestoreUser(ctx, 17, 100))
	a.Chunk = 2
	require.NoError(t, repo.ApplyWatchAward(ctx, a))
	_, found, err = repo.BalanceGet(ctx, 17, 77)
	require.NoError(t, err)
	require.False(t, found)
	require.NoError(t, repo.RestoreUser(ctx, 17, 101))
	require.NoError(t, repo.DeleteAccount(ctx, 17, 100))
	a.AccountCreatedAt = 101
	require.NoError(t, repo.ApplyWatchAward(ctx, a))
	row, found, err := repo.BalanceGet(ctx, 17, 77)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 10, row.Points)
	a.AccountCreatedAt = 100
	a.Chunk = 3
	require.NoError(t, repo.ApplyWatchAward(ctx, a))
	row, _, err = repo.BalanceGet(ctx, 17, 77)
	require.NoError(t, err)
	require.EqualValues(t, 10, row.Points)
}
func TestWatchFailedPostingRemainsReplayable(t *testing.T) {
	repo, raw := sqliteRetentionRepo(t)
	ctx := context.Background()
	a := watchAward(17, 77, 100)
	_, _, err := repo.BalanceAdjustViewer(ctx, loyaltyrepo.BalanceAdjustment{UserID: 17, ViewerID: 77, ViewerLogin: "viewer", Value: 100, Absolute: true})
	require.NoError(t, err)
	_, err = raw.ExecContext(ctx, "CREATE TRIGGER fail_watch BEFORE UPDATE ON balances BEGIN SELECT RAISE(ABORT, 'posting failure'); END")
	require.NoError(t, err)
	require.ErrorContains(t, repo.ApplyWatchAward(ctx, a), "posting failure")
	row, found, err := repo.BalanceGet(ctx, 17, 77)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 100, row.Points)
	require.Zero(t, row.WatchSeconds)
	_, err = raw.ExecContext(ctx, "DROP TRIGGER fail_watch")
	require.NoError(t, err)
	require.NoError(t, repo.ApplyWatchAward(ctx, a))
	row, _, err = repo.BalanceGet(ctx, 17, 77)
	require.NoError(t, err)
	require.EqualValues(t, 110, row.Points)
	require.EqualValues(t, 300, row.WatchSeconds)
}

func TestUnknownDeletionCannotRetireKnownAccount(t *testing.T) {
	repo, _ := newLoyaltyRepo(t)
	ctx := t.Context()
	require.NoError(t, repo.EnsureWatchSchema(ctx))
	require.NoError(t, repo.RestoreUser(ctx, 17, 101))
	a := watchAward(17, 77, 101)
	require.NoError(t, repo.ApplyWatchAward(ctx, a))
	require.NoError(t, repo.DeleteAccount(ctx, 17, 0))
	row, found, err := repo.BalanceGet(ctx, 17, 77)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 10, row.Points)
	a.WindowID = "next"
	require.NoError(t, repo.ApplyWatchAward(ctx, a))
	row, found, err = repo.BalanceGet(ctx, 17, 77)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 20, row.Points)
}

func TestWatchDeletionFencesCounterBatches(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	ctx := t.Context()
	require.NoError(t, repo.EnsureWatchSchema(ctx))
	require.NoError(t, repo.RestoreUser(ctx, 17, 100))
	require.NoError(t, repo.DeleteAccount(ctx, 17, 100))
	require.NoError(t, repo.ApplyBumps(ctx, data.CounterBumpedDTO{
		UserID: 17, BatchID: "retired-counter-batch",
		Bumps: []data.CounterBumpEntry{{Name: "deaths", Scope: data.CounterScopeChannel, Delta: 1}},
	}))
	_, _, found, err := repo.CounterGet(ctx, 17, "deaths", 0, "")
	require.NoError(t, err)
	require.False(t, found)
	// The discarded delivery keeps its replay marker: a lost ACK can redeliver after recreation.
	require.Equal(t, 1, client.CounterBatch.Query().CountX(ctx))
	require.NoError(t, repo.RestoreUser(ctx, 17, 101))
	require.NoError(t, repo.ApplyBumps(ctx, data.CounterBumpedDTO{
		UserID: 17, BatchID: "retired-counter-batch",
		Bumps: []data.CounterBumpEntry{{Name: "deaths", Scope: data.CounterScopeChannel, Delta: 1}},
	}))
	_, _, found, err = repo.CounterGet(ctx, 17, "deaths", 0, "")
	require.NoError(t, err)
	require.False(t, found, "a discarded batch cannot pay a recreated account")
	require.Equal(t, 1, client.CounterBatch.Query().CountX(ctx))
}
