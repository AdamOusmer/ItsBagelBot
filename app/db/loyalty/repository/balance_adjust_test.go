// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"math"
	"strings"
	"testing"

	"ItsBagelBot/app/db/loyalty/ent/balance"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	baseent "entgo.io/ent"
	"github.com/stretchr/testify/require"
)

func TestBalanceAdjustViewerCreatesFirstBalance(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    int64
		absolute bool
		points   int64
	}{
		{"set", 5000, true, 5000},
		{"add", 5000, false, 5000},
		{"remove", -5000, false, 0},
		{"negative set", -5000, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _ := newLoyaltyRepo(t)
			row, found, err := repo.BalanceAdjustViewer(t.Context(), loyaltyrepo.BalanceAdjustment{UserID: 2, ViewerID: 8, ViewerLogin: " @Blemmyz ", Value: tc.value, Absolute: tc.absolute})
			require.NoError(t, err)
			require.True(t, found)
			require.Equal(t, tc.points, row.Points)
			require.EqualValues(t, 8, row.ViewerID)
			require.Equal(t, "blemmyz", row.ViewerLogin)
			require.Zero(t, row.WatchSeconds)
		})
	}
}

func TestBalanceAdjustViewerUsesStableIDAndPreservesMetadata(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 8, Login: "oldlogin", Points: 100})
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 9, Login: "blemmyz", Points: 70})
	seedBalance(t, client, seedRow{UserID: 3, ViewerID: 8, Login: "blemmyz", Points: 50})
	require.NoError(t, client.Balance.Update().Where(balance.UserIDEQ(2), balance.ViewerIDEQ(8)).
		SetViewerName("Blemmyz").SetWatchSeconds(600).Exec(t.Context()))
	for _, tc := range []struct {
		value    int64
		absolute bool
		points   int64
	}{
		{40, false, 140}, {75, true, 75}, {-100, false, 0}, {20, false, 20},
	} {
		row, found, err := repo.BalanceAdjustViewer(t.Context(), loyaltyrepo.BalanceAdjustment{UserID: 2, ViewerID: 8, ViewerLogin: "blemmyz", Value: tc.value, Absolute: tc.absolute})
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, tc.points, row.Points)
		require.Equal(t, "Blemmyz", row.ViewerName)
		require.EqualValues(t, 600, row.WatchSeconds)
		require.Equal(t, "blemmyz", row.ViewerLogin)
	}
	stale, _, err := repo.BalanceGet(t.Context(), 2, 9)
	require.NoError(t, err)
	require.EqualValues(t, 70, stale.Points, "reassigned login must not select the old account")
	other, _, err := repo.BalanceGet(t.Context(), 3, 8)
	require.NoError(t, err)
	require.EqualValues(t, 50, other.Points)
}

func TestBalanceAdjustViewerDeltaIncludesInterleavedEarnings(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	seedBalance(t, client, seedRow{UserID: 2, ViewerID: 8, Login: "blemmyz", Points: 100})
	// An accrual lands after the adjustment is prepared and before the upsert.
	// The adjustment must add to the current SQL value, never a prior snapshot.
	client.Balance.Use(func(next baseent.Mutator) baseent.Mutator {
		return baseent.MutateFunc(func(ctx context.Context, m baseent.Mutation) (baseent.Value, error) {
			if m.Op().Is(baseent.OpCreate) {
				if err := client.Balance.Update().Where(balance.UserIDEQ(2), balance.ViewerIDEQ(8)).
					AddPoints(10).AddWatchSeconds(300).Exec(ctx); err != nil {
					return nil, err
				}
			}
			return next.Mutate(ctx, m)
		})
	})
	row, found, err := repo.BalanceAdjustViewer(t.Context(), loyaltyrepo.BalanceAdjustment{UserID: 2, ViewerID: 8, ViewerLogin: "blemmyz", Value: 40})
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 150, row.Points)
	require.EqualValues(t, 300, row.WatchSeconds)
}

func TestBalanceAdjustViewerRejectsInvalidIdentity(t *testing.T) {
	repo, client := newLoyaltyRepo(t)
	for _, tc := range []struct {
		userID, viewerID uint64
		login            string
	}{
		{0, 8, "blemmyz"}, {2, 0, "blemmyz"}, {2, 8, " @ "}, {2, 8, strings.Repeat("b", 65)},
	} {
		_, found, err := repo.BalanceAdjustViewer(t.Context(), loyaltyrepo.BalanceAdjustment{UserID: tc.userID, ViewerID: tc.viewerID, ViewerLogin: tc.login, Value: 50})
		require.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
		require.False(t, found)
	}
	count, err := client.Balance.Query().Count(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestBalanceAdjustViewerSignedBigIntRange(t *testing.T) {
	repo, _ := newLoyaltyRepo(t)
	target := loyaltyrepo.BalanceAdjustment{UserID: 2, ViewerID: 8, ViewerLogin: "blemmyz", Value: math.MaxInt64 - 1, Absolute: true}
	row, found, err := repo.BalanceAdjustViewer(t.Context(), target)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, math.MaxInt64-1, row.Points)
	target.Absolute, target.Value = false, 1
	row, found, err = repo.BalanceAdjustViewer(t.Context(), target)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, math.MaxInt64, row.Points)
	_, found, err = repo.BalanceAdjustViewer(t.Context(), target)
	require.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)
	require.False(t, found)
	row, _, err = repo.BalanceGet(t.Context(), 2, 8)
	require.NoError(t, err)
	require.EqualValues(t, math.MaxInt64, row.Points, "overflow must roll back without losing points")
	target.Value = math.MinInt64
	row, found, err = repo.BalanceAdjustViewer(t.Context(), target)
	require.NoError(t, err)
	require.True(t, found)
	require.Zero(t, row.Points)
	target.Value = math.MaxInt64
	row, found, err = repo.BalanceAdjustViewer(t.Context(), target)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, math.MaxInt64, row.Points)
}
