// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBalanceWagerRefreshesTheStoredLogin(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "oldname", Points: 100})

	out, err := repo.BalanceWager(t.Context(), loyaltyrepo.Wager{UserID: 2, ViewerID: 7, ViewerLogin: "NewName", Amount: 10, Won: true})

	require.NoError(t, err)
	assert.True(t, out.Applied)
	assert.EqualValues(t, 110, out.Balance.Points)
	assert.Equal(t, "newname", out.Balance.ViewerLogin)
}

func TestBalanceTransferByIDIgnoresAStoredLoginMismatch(t *testing.T) {
	repo, client, ctx := fundedLoyalty(t, 100)
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 8, Login: "oldname", Points: 5})
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 9, Login: "newname", Points: 5})

	out, found, err := repo.BalanceTransfer(ctx, loyaltyrepo.Transfer{UserID: 2, FromViewerID: 7, TargetViewerID: 8, TargetLogin: "newname", Amount: 40})

	require.NoError(t, err)
	require.True(t, found)
	assert.EqualValues(t, 45, out.To.Points)
	assert.Equal(t, "newname", out.To.ViewerLogin)
	bystander, _, err := repo.BalanceGet(ctx, 2, 9)
	require.NoError(t, err)
	assert.EqualValues(t, 5, bystander.Points)
}

func TestBalanceAdjustViewerByIDIgnoresAStoredLoginMismatch(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 8, Login: "oldname", Points: 5})
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 9, Login: "newname", Points: 5})

	row, found, err := repo.BalanceAdjustViewer(t.Context(), loyaltyrepo.BalanceAdjustment{UserID: 2, ViewerID: 8, ViewerLogin: "newname", Value: 10})

	require.NoError(t, err)
	require.True(t, found)
	assert.EqualValues(t, 15, row.Points)
	bystander, _, err := repo.BalanceGet(t.Context(), 2, 9)
	require.NoError(t, err)
	assert.EqualValues(t, 5, bystander.Points)
}
