// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"database/sql"
	"math"
	"testing"

	"ItsBagelBot/app/db/loyalty/ent/balance"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"

	"github.com/stretchr/testify/require"
)

func TestBalanceWagerAtomicOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		initial, amount       int64
		won, applied, limited bool
		points                int64
	}{
		{name: "win", initial: 100, amount: 30, won: true, applied: true, points: 130},
		{name: "loss", initial: 100, amount: 30, applied: true, points: 70},
		{name: "loss all", initial: math.MaxInt64, amount: math.MaxInt64, applied: true, points: 0},
		{name: "shortfall win", initial: 10, amount: 30, won: true, points: 10},
		{name: "shortfall loss", initial: 10, amount: 30, points: 10},
		{name: "headroom refusal", initial: math.MaxInt64, amount: 1, won: true, limited: true, points: math.MaxInt64},
		{name: "exact headroom", initial: math.MaxInt64 - 10, amount: 10, won: true, applied: true, points: math.MaxInt64},
		{name: "large exact win", initial: math.MaxInt64 / 2, amount: math.MaxInt64 / 2, won: true, applied: true, points: math.MaxInt64 - 1},
		{name: "full range win refused", initial: math.MaxInt64, amount: math.MaxInt64, won: true, limited: true, points: math.MaxInt64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, client := newLoyaltyRepo(t)
			seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "oldlogin", Points: tc.initial})
			seedBalance(t, client, seedRow{UserID: 3, ViewerID: 7, Login: "oldlogin", Points: 100})
			require.NoError(t, client.Balance.Update().Where(balance.UserIDEQ(2)).SetViewerName("Viewer").SetWatchSeconds(300).Exec(t.Context()))
			outcome, err := repo.BalanceWager(t.Context(), loyaltyrepo.Wager{UserID: 2, ViewerID: 7, Amount: tc.amount, Won: tc.won})
			require.NoError(t, err)
			require.True(t, outcome.Found)
			require.Equal(t, tc.applied, outcome.Applied)
			require.Equal(t, tc.limited, outcome.LimitExceeded)
			require.Equal(t, tc.points, outcome.Balance.Points)
			require.Equal(t, "oldlogin", outcome.Balance.ViewerLogin)
			require.Equal(t, "Viewer", outcome.Balance.ViewerName)
			require.EqualValues(t, 300, outcome.Balance.WatchSeconds)
			other, _, err := repo.BalanceGet(t.Context(), 3, 7)
			require.NoError(t, err)
			require.EqualValues(t, 100, other.Points)
		})
	}
}

func TestBalanceWagerMissingAndInvalid(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	outcome, err := repo.BalanceWager(t.Context(), loyaltyrepo.Wager{UserID: 2, ViewerID: 7, Amount: 1, Won: true})
	require.NoError(t, err)
	require.False(t, outcome.Found)
	require.False(t, outcome.Applied)
	require.Nil(t, outcome.Balance)
	for _, wager := range []loyaltyrepo.Wager{
		{UserID: 0, ViewerID: 7, Amount: 1},
		{UserID: 2, ViewerID: 0, Amount: 1},
		{UserID: 2, ViewerID: 7, Amount: 0},
		{UserID: 2, ViewerID: 7, Amount: -1},
	} {
		_, err := repo.BalanceWager(t.Context(), wager)
		require.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
	}
	count, err := client.Balance.Query().Count(t.Context())
	require.NoError(t, err)
	require.Zero(t, count, "wagers must never create an unknown balance")
}

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
