// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"crypto/sha256"
	"math"
	"testing"
	"time"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"github.com/stretchr/testify/require"
)

func TestBalanceCreditReplaysOriginalStanding(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*loyaltyrepo.BalanceCredit)
		wantErr error
	}{
		{name: "same payload", change: func(*loyaltyrepo.BalanceCredit) {}},
		{name: "different value", change: func(c *loyaltyrepo.BalanceCredit) { c.Value++ }, wantErr: loyaltyrepo.ErrCreditKeyMismatch},
		{name: "different viewer", change: func(c *loyaltyrepo.BalanceCredit) { c.ViewerID++ }, wantErr: loyaltyrepo.ErrCreditKeyMismatch},
		{name: "different display login", change: func(c *loyaltyrepo.BalanceCredit) { c.ViewerLogin = "newlogin" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := sqliteRetentionRepo(t)
			ctx := t.Context()
			require.NoError(t, r.EnsureBalanceCreditSchema(ctx))
			credit := loyaltyrepo.BalanceCredit{UserID: 42, ViewerID: 7, ViewerLogin: "Viewer", Value: 10, Key: "raffle:one:1"}
			first, err := r.BalanceCredit(ctx, credit)
			require.NoError(t, err)
			require.False(t, first.Replayed)
			require.EqualValues(t, 10, first.Balance.Points)
			_, _, err = r.BalanceAdjustViewer(ctx, loyaltyrepo.BalanceAdjustment{UserID: 42, ViewerID: 7, ViewerLogin: "updated", Value: 20})
			require.NoError(t, err)

			tc.change(&credit)
			result, err := r.BalanceCredit(ctx, credit)
			require.ErrorIs(t, err, tc.wantErr)
			if tc.wantErr == nil {
				require.Equal(t, loyaltyrepo.CreditResult{Balance: first.Balance, Replayed: true}, result)
			}
			row, _, err := r.BalanceGet(ctx, 42, 7)
			require.NoError(t, err)
			require.EqualValues(t, 30, row.Points)
		})
	}
}

func TestBalanceCreditFencesAccountInstances(t *testing.T) {
	for _, tc := range []struct {
		name, key           string
		expected, recreated int64
		retire              bool
		wantErr             error
	}{
		{name: "current instance", key: "raffle:one:2", expected: 10},
		{name: "future instance", key: "raffle:one:2", expected: 11, wantErr: loyaltyrepo.ErrCreditAccountMismatch},
		{name: "retired account", key: "raffle:one:1", expected: 10, retire: true, wantErr: loyaltyrepo.ErrCreditAccountRetired},
		{name: "old instance replay", key: "raffle:one:1", expected: 10, retire: true, recreated: 11, wantErr: loyaltyrepo.ErrCreditAccountMismatch},
		{name: "zero instance with unused key", key: "raffle:one:2", retire: true, recreated: 11, wantErr: loyaltyrepo.ErrCreditAccountMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := sqliteRetentionRepo(t)
			ctx := t.Context()
			require.NoError(t, r.EnsureBalanceCreditSchema(ctx))
			require.NoError(t, r.RestoreUser(ctx, 42, 10))
			credit := loyaltyrepo.BalanceCredit{UserID: 42, ViewerID: 7, Value: 10, Key: "raffle:one:1", AccountCreatedAt: 10}
			_, err := r.BalanceCredit(ctx, credit)
			require.NoError(t, err)

			if tc.retire {
				require.NoError(t, r.DeleteAccount(ctx, 42, 10))
			}
			if tc.recreated != 0 {
				require.NoError(t, r.RestoreUser(ctx, 42, tc.recreated))
			}
			credit.AccountCreatedAt = tc.expected
			credit.Key = tc.key
			_, err = r.BalanceCredit(ctx, credit)
			require.ErrorIs(t, err, tc.wantErr)
			_, found, err := r.BalanceGet(ctx, 42, 7)
			require.NoError(t, err)
			require.Equal(t, !tc.retire, found)
		})
	}
}

func TestBalanceCreditReplaysAfterFirstWatchAward(t *testing.T) {
	r, _ := sqliteRetentionRepo(t)
	ctx := t.Context()
	require.NoError(t, r.EnsureBalanceCreditSchema(ctx))
	credit := loyaltyrepo.BalanceCredit{UserID: 42, ViewerID: 7, Value: 10, Key: "raffle:new:1", AccountCreatedAt: 10}
	first, err := r.BalanceCredit(ctx, credit)
	require.NoError(t, err)
	require.NoError(t, r.ApplyWatchAward(ctx, watchAward(credit.UserID, credit.ViewerID, credit.AccountCreatedAt)))

	replay, err := r.BalanceCredit(ctx, credit)
	require.NoError(t, err)
	require.Equal(t, loyaltyrepo.CreditResult{Balance: first.Balance, Replayed: true}, replay)

	standing, found, err := r.BalanceGet(ctx, 42, 7)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, int64(20), standing.Points)
}

func TestBalanceCreditPrunesOnlyExpiredMarkers(t *testing.T) {
	r, raw := sqliteRetentionRepo(t)
	ctx := t.Context()
	require.NoError(t, r.EnsureBalanceCreditSchema(ctx))
	credit := loyaltyrepo.BalanceCredit{UserID: 42, ViewerID: 7, Value: 10, Key: "raffle:one:1"}
	_, err := r.BalanceCredit(ctx, credit)
	require.NoError(t, err)
	_, err = raw.ExecContext(ctx, "UPDATE loyalty_balance_credits SET created_at=?", time.Now().Add(-25*time.Hour-time.Minute).UnixMilli())
	require.NoError(t, err)
	credit.Key = "raffle:one:2"
	_, err = r.BalanceCredit(ctx, credit)
	require.NoError(t, err)
	keyHash := sha256.Sum256([]byte(credit.Key))
	_, err = raw.ExecContext(ctx, "UPDATE loyalty_balance_credits SET created_at=? WHERE key_hash=?",
		time.Now().Add(-24*time.Hour-30*time.Minute).UnixMilli(), keyHash[:])
	require.NoError(t, err)

	require.NoError(t, r.PruneBalanceCredits(ctx))
	for _, tc := range []struct {
		key      string
		points   int64
		replayed bool
	}{{"raffle:one:1", 30, false}, {"raffle:one:2", 20, true}} {
		credit.Key = tc.key
		got, err := r.BalanceCredit(ctx, credit)
		require.NoError(t, err)
		require.Equal(t, tc.replayed, got.Replayed)
		require.Equal(t, tc.points, got.Balance.Points)
	}
}

func TestBalanceCreditOverflowLeavesKeyAvailable(t *testing.T) {
	r, _ := sqliteRetentionRepo(t)
	ctx := t.Context()
	require.NoError(t, r.EnsureBalanceCreditSchema(ctx))
	credit := loyaltyrepo.BalanceCredit{UserID: 42, ViewerID: 7, Value: math.MaxInt64, Key: "raffle:one:1"}
	_, err := r.BalanceCredit(ctx, credit)
	require.NoError(t, err)
	credit.Key = "raffle:one:2"
	_, err = r.BalanceCredit(ctx, credit)
	require.ErrorIs(t, err, loyaltyrepo.ErrInvalidInput)

	_, _, err = r.BalanceAdjustViewer(ctx, loyaltyrepo.BalanceAdjustment{UserID: 42, ViewerID: 7, ViewerLogin: "viewer", Absolute: true})
	require.NoError(t, err)
	got, err := r.BalanceCredit(ctx, credit)
	require.NoError(t, err)
	require.EqualValues(t, math.MaxInt64, got.Balance.Points)
	require.Equal(t, "viewer", got.Balance.ViewerLogin)
}
