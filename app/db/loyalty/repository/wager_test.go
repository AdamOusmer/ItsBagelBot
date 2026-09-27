// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
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
