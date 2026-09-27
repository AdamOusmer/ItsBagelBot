// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"database/sql"
	"math"
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"github.com/stretchr/testify/require"
)

func TestBalanceAdjustViewerSQLiteConcurrentAccrual(t *testing.T) {
	repo, raw := sqliteRetentionRepo(t)
	checkConcurrentAdjustmentAccrual(t, repo, raw)
}

func TestBalanceAdjustViewerMySQLConcurrentAccrual(t *testing.T) {
	repo, raw := mysqlRetentionRepo(t)
	checkConcurrentAdjustmentAccrual(t, repo, raw)
}

func checkConcurrentAdjustmentAccrual(t *testing.T, repo *loyaltyrepo.Loyalty, raw *sql.DB) {
	t.Helper()
	target := loyaltyrepo.BalanceAdjustment{UserID: 2, ViewerID: 8, ViewerLogin: "blemmyz"}
	_, found, err := repo.BalanceAdjustViewer(t.Context(), target)
	require.NoError(t, err)
	require.True(t, found)
	const changes = 20
	errors := make(chan error, changes*2)
	start := make(chan struct{})
	for range changes {
		go func() {
			<-start
			adjustment := target
			adjustment.Value = 1
			_, _, err := repo.BalanceAdjustViewer(t.Context(), adjustment)
			errors <- err
		}()
		go func() {
			<-start
			_, err := raw.ExecContext(t.Context(), "UPDATE balances SET points=points+1,watch_seconds=watch_seconds+3 WHERE user_id=? AND viewer_id=?", 2, 8)
			errors <- err
		}()
	}
	close(start)
	for range changes * 2 {
		require.NoError(t, <-errors)
	}
	row, _, err := repo.BalanceGet(t.Context(), 2, 8)
	require.NoError(t, err)
	require.EqualValues(t, changes*2, row.Points)
	require.EqualValues(t, changes*3, row.WatchSeconds)
	target.Absolute, target.Value = true, math.MaxInt64
	row, _, err = repo.BalanceAdjustViewer(t.Context(), target)
	require.NoError(t, err)
	require.EqualValues(t, math.MaxInt64, row.Points)
	target.Absolute, target.Value = false, 1
	_, _, err = repo.BalanceAdjustViewer(t.Context(), target)
	require.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
	row, _, err = repo.BalanceGet(t.Context(), 2, 8)
	require.NoError(t, err)
	require.EqualValues(t, math.MaxInt64, row.Points)
	require.EqualValues(t, changes*3, row.WatchSeconds)
}
