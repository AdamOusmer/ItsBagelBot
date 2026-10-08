// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"fmt"
	"math"
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/event/data"

	"github.com/stretchr/testify/assert"
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

func TestWatchPointsCapConcurrentSet(t *testing.T) {
	repo, _ := sqliteRetentionRepo(t)
	checkWatchPointsCapConcurrentSet(t, repo)
}

func TestWatchPointsCapConcurrentSetMySQL(t *testing.T) {
	repo, _ := mysqlRetentionRepo(t)
	checkWatchPointsCapConcurrentSet(t, repo)
}

func checkWatchPointsCapConcurrentSet(t *testing.T, repo *loyaltyrepo.Loyalty) {
	t.Helper()
	require.NoError(t, repo.RestoreUser(t.Context(), 17, 100))
	target := loyaltyrepo.BalanceAdjustment{UserID: 17, ViewerID: 77, ViewerLogin: "viewer", Value: math.MaxInt64, Absolute: true}
	_, _, err := repo.BalanceAdjustViewer(t.Context(), target)
	require.NoError(t, err)
	const windows = 20
	start := make(chan struct{})
	errors := make(chan error, windows*2)
	for i := range windows {
		go func() {
			<-start
			_, _, err := repo.BalanceAdjustViewer(t.Context(), target)
			errors <- err
		}()
		go func(window int) {
			<-start
			award := watchAward(17, 77, 100)
			award.WindowID = fmt.Sprintf("window-%d", window)
			award.Entries = append(award.Entries, data.LoyaltyEarnEntry{ViewerID: 78, Points: 10, WatchSeconds: 300})
			errors <- repo.ApplyWatchAward(t.Context(), award)
		}(i)
	}
	close(start)
	for range windows * 2 {
		require.NoError(t, <-errors)
	}
	capped, _, err := repo.BalanceGet(t.Context(), 17, 77)
	require.NoError(t, err)
	require.EqualValues(t, math.MaxInt64, capped.Points)
	require.EqualValues(t, windows*300, capped.WatchSeconds)
	other, _, err := repo.BalanceGet(t.Context(), 17, 78)
	require.NoError(t, err)
	require.EqualValues(t, windows*10, other.Points)
	require.EqualValues(t, windows*300, other.WatchSeconds)
}

type earnedRow struct {
	login, name  string
	points       int64
	watchSeconds uint64
}

func TestRecordEarnedFlushFoldsPerViewerAndDropsEmptyEntries(t *testing.T) {
	repo, _ := sqliteRetentionRepo(t)
	require.NoError(t, repo.RestoreUser(t.Context(), 1, 100))
	repo.RecordEarned(data.LoyaltyEarnedDTO{UserID: 1, Entries: []data.LoyaltyEarnEntry{
		{ViewerID: 7, ViewerLogin: "cool", Points: 100, WatchSeconds: 300},
		{ViewerID: 7, ViewerName: "Cool", Points: 50},
		{ViewerID: 8, WatchSeconds: 300},
		{ViewerID: 0, Points: 10},
		{ViewerID: 9},
	}})

	repo.Flush(t.Context())
	repo.Flush(t.Context())

	for viewerID, want := range map[uint64]*earnedRow{
		7: {login: "cool", name: "Cool", points: 150, watchSeconds: 300},
		8: {watchSeconds: 300},
		9: nil,
		0: nil,
	} {
		row, found, err := repo.BalanceGet(t.Context(), 1, viewerID)
		require.NoError(t, err)
		require.Equal(t, want != nil, found, "viewer %d", viewerID)
		if found {
			assert.Equal(t, *want, earnedRow{row.ViewerLogin, row.ViewerName, row.Points, row.WatchSeconds}, "a drained window must not apply twice")
		}
	}
}
