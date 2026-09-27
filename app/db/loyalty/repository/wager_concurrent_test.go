// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"database/sql"
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"github.com/stretchr/testify/require"
)

func TestBalanceWagerConcurrentSpendsAndAccrual(t *testing.T) {
	repo, raw := sqliteRetentionRepo(t)
	checkConcurrentWagerSpends(t, repo, raw)
}

func TestBalanceWagerConcurrentSpendsAndAccrualMySQL(t *testing.T) {
	repo, raw := mysqlRetentionRepo(t)
	checkConcurrentWagerSpends(t, repo, raw)
}

type wagerChange struct {
	debit int64
	err   error
}

func checkConcurrentWagerSpends(t *testing.T, repo *loyaltyrepo.Loyalty, raw *sql.DB) {
	t.Helper()
	_, _, err := repo.BalanceAdjustViewer(t.Context(), loyaltyrepo.BalanceAdjustment{UserID: 2, ViewerID: 7, ViewerLogin: "sender", Value: 50, Absolute: true})
	require.NoError(t, err)
	const changes = 20
	start := make(chan struct{})
	results := make(chan wagerChange, changes*3)
	for range changes {
		go func() {
			<-start
			outcome, err := repo.BalanceWager(t.Context(), loyaltyrepo.Wager{UserID: 2, ViewerID: 7, Amount: 7})
			result := wagerChange{err: err}
			if outcome.Applied {
				result.debit = 7
			}
			results <- result
		}()
		go func() {
			<-start
			_, _, spent, err := repo.BalanceSpend(t.Context(), 2, "sender", 3)
			result := wagerChange{err: err}
			if spent {
				result.debit = 3
			}
			results <- result
		}()
		go func() {
			<-start
			_, err := raw.ExecContext(t.Context(), "UPDATE balances SET points=points+1,watch_seconds=watch_seconds+10 WHERE user_id=? AND viewer_id=?", 2, 7)
			results <- wagerChange{err: err}
		}()
	}
	close(start)
	var debited int64
	for range changes * 3 {
		result := <-results
		require.NoError(t, result.err)
		debited += result.debit
	}
	row, _, err := repo.BalanceGet(t.Context(), 2, 7)
	require.NoError(t, err)
	require.EqualValues(t, 50+changes-debited, row.Points)
	require.GreaterOrEqual(t, row.Points, int64(0))
	require.EqualValues(t, changes*10, row.WatchSeconds)
}
