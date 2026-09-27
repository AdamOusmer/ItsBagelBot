// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"fmt"
	"math"
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/event/data"
	"github.com/stretchr/testify/require"
)

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
