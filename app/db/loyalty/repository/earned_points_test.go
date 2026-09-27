// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"math"
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/event/data"
	"github.com/stretchr/testify/require"
)

func TestWatchPointsCapKeepsWatchTimeAndOtherViewers(t *testing.T) {
	repo, _ := sqliteRetentionRepo(t)
	checkWatchPointsCap(t, repo)
}

func TestWatchPointsCapMySQL(t *testing.T) {
	repo, _ := mysqlRetentionRepo(t)
	checkWatchPointsCap(t, repo)
}

func checkWatchPointsCap(t *testing.T, repo *loyaltyrepo.Loyalty) {
	t.Helper()
	_, _, err := repo.BalanceAdjustViewer(t.Context(), loyaltyrepo.BalanceAdjustment{UserID: 17, ViewerID: 77, ViewerLogin: "viewer", Value: math.MaxInt64 - 5, Absolute: true})
	require.NoError(t, err)
	award := watchAward(17, 77, 100)
	award.Entries = append(award.Entries, data.LoyaltyEarnEntry{ViewerID: 78, ViewerLogin: "other", Points: 10, WatchSeconds: 300})
	require.NoError(t, repo.ApplyWatchAward(t.Context(), award))
	require.NoError(t, repo.ApplyWatchAward(t.Context(), award), "cap must still commit deduplication markers")
	capped, _, err := repo.BalanceGet(t.Context(), 17, 77)
	require.NoError(t, err)
	require.EqualValues(t, math.MaxInt64, capped.Points)
	require.EqualValues(t, 300, capped.WatchSeconds)
	other, _, err := repo.BalanceGet(t.Context(), 17, 78)
	require.NoError(t, err)
	require.EqualValues(t, 10, other.Points)
	require.EqualValues(t, 300, other.WatchSeconds)
	award.WindowID = "next"
	require.NoError(t, repo.ApplyWatchAward(t.Context(), award))
	capped, _, err = repo.BalanceGet(t.Context(), 17, 77)
	require.NoError(t, err)
	require.EqualValues(t, math.MaxInt64, capped.Points)
	require.EqualValues(t, 600, capped.WatchSeconds)
}

func TestLegacyEarnPointsCapKeepsOtherViewers(t *testing.T) {
	repo, _ := sqliteRetentionRepo(t)
	checkLegacyEarnPointsCap(t, repo)
}

func TestLegacyEarnPointsCapMySQL(t *testing.T) {
	repo, _ := mysqlRetentionRepo(t)
	checkLegacyEarnPointsCap(t, repo)
}

func checkLegacyEarnPointsCap(t *testing.T, repo *loyaltyrepo.Loyalty) {
	t.Helper()
	require.NoError(t, repo.RestoreUser(t.Context(), 17, 100))
	_, _, err := repo.BalanceAdjustViewer(t.Context(), loyaltyrepo.BalanceAdjustment{UserID: 17, ViewerID: 77, ViewerLogin: "viewer", Value: math.MaxInt64 - 5, Absolute: true})
	require.NoError(t, err)
	repo.RecordEarned(data.LoyaltyEarnedDTO{UserID: 17, Entries: []data.LoyaltyEarnEntry{
		{ViewerID: 77, Points: 10, WatchSeconds: 300},
		{ViewerID: 78, Points: 20, WatchSeconds: 300},
		{ViewerID: 79, Points: math.MaxInt64 - 1},
		{ViewerID: 79, Points: 10},
	}})
	repo.Flush(t.Context())
	capped, _, err := repo.BalanceGet(t.Context(), 17, 77)
	require.NoError(t, err)
	require.EqualValues(t, math.MaxInt64, capped.Points)
	require.EqualValues(t, 300, capped.WatchSeconds)
	other, found, err := repo.BalanceGet(t.Context(), 17, 78)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 20, other.Points)
	require.EqualValues(t, 300, other.WatchSeconds)
	aggregate, found, err := repo.BalanceGet(t.Context(), 17, 79)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, math.MaxInt64, aggregate.Points, "buffered sum must not wrap before SQL")
}

func TestLegacyBalanceAdjustRejectsOverflow(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedBalance(t, client, seedRow{UserID: 17, ViewerID: 77, Login: "viewer", Points: math.MaxInt64})
	_, found, err := repo.BalanceAdjust(t.Context(), 17, "viewer", 1, false)
	require.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
	require.False(t, found)
	row, _, err := repo.BalanceGet(t.Context(), 17, 77)
	require.NoError(t, err)
	require.EqualValues(t, math.MaxInt64, row.Points)
	_, found, err = repo.BalanceAdjust(t.Context(), 17, "unseen", 1, false)
	require.NoError(t, err)
	require.False(t, found)
}
