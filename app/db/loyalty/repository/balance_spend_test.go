// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBalanceSpendViewerFollowsTheIDAcrossARename(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "oldname", Points: 100})
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 8, Login: "newname", Points: 100})

	row, found, spent, err := repo.BalanceSpendViewer(t.Context(), loyaltyrepo.Spend{UserID: 2, ViewerID: 7, ViewerLogin: "@NewName2", Amount: 30})

	require.NoError(t, err)
	assert.True(t, found)
	assert.True(t, spent)
	assert.EqualValues(t, 70, row.Points)
	assert.Equal(t, "newname2", row.ViewerLogin, "the login only refreshes the display name")
	other, _, err := repo.BalanceGet(t.Context(), 2, 8)
	require.NoError(t, err)
	assert.EqualValues(t, 100, other.Points, "the viewer who now owns the old login is untouched")
}

func TestBalanceSpendViewerRefusals(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 7, Login: "sender", Points: 10})

	_, found, _, err := repo.BalanceSpendViewer(t.Context(), loyaltyrepo.Spend{UserID: 2, ViewerID: 99, ViewerLogin: "sender", Amount: 5})
	require.NoError(t, err)
	assert.False(t, found, "an unknown ID is not found even when its login matches a stored row")

	row, found, spent, err := repo.BalanceSpendViewer(t.Context(), loyaltyrepo.Spend{UserID: 2, ViewerID: 7, ViewerLogin: "renamed", Amount: 50})
	require.NoError(t, err)
	assert.True(t, found)
	assert.False(t, spent)
	assert.EqualValues(t, 10, row.Points)
	assert.Equal(t, "sender", row.ViewerLogin, "a refused spend keeps the stored login")

	for _, spend := range []loyaltyrepo.Spend{{UserID: 0, ViewerID: 7, Amount: 1}, {UserID: 2, ViewerID: 0, Amount: 1}, {UserID: 2, ViewerID: 7, Amount: 0}} {
		_, _, _, err := repo.BalanceSpendViewer(t.Context(), spend)
		assert.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
	}
}

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
