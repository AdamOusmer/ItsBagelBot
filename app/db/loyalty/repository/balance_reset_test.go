// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	"ItsBagelBot/app/db/loyalty/ent/balance"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"

	"github.com/stretchr/testify/require"
)

func TestBalanceResetZeroesOnlyOneChannel(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	for _, row := range []seedRow{
		{UserID: 2, ViewerID: 7, Login: "a", Points: 500},
		{UserID: 2, ViewerID: 8, Login: "b", Points: 10},
		{UserID: 2, ViewerID: 9, Login: "c", Points: 0},
		{UserID: 3, ViewerID: 7, Login: "a", Points: 700},
	} {
		seedBalance(t, client, row)
	}
	require.NoError(t, client.Balance.Update().SetWatchSeconds(900).Exec(t.Context()))

	reset, err := repo.BalanceReset(t.Context(), 2)
	require.NoError(t, err)
	require.EqualValues(t, 2, reset)

	rows, err := client.Balance.Query().Where(balance.UserIDEQ(2)).All(t.Context())
	require.NoError(t, err)
	require.Len(t, rows, 3)
	for _, row := range rows {
		require.Zero(t, row.Points)
		require.EqualValues(t, 900, row.WatchSeconds)
	}
	other, found, err := repo.BalanceGet(t.Context(), 3, 7)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 700, other.Points)
	require.EqualValues(t, 900, other.WatchSeconds)

	again, err := repo.BalanceReset(t.Context(), 2)
	require.NoError(t, err)
	require.Zero(t, again)
}

func TestBalanceResetRefusesZeroUser(t *testing.T) {
	repo, _ := newLoyaltyRepo(t)

	_, err := repo.BalanceReset(t.Context(), 0)

	require.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
}
