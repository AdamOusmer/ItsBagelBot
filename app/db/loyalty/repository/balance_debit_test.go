// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"strconv"
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"github.com/stretchr/testify/require"
)

func TestBalanceDebitReplaysOriginalOutcome(t *testing.T) {
	for _, tc := range []struct {
		name      string
		seed, end int64
		want      loyaltyrepo.DebitResult
	}{
		{name: "spent", seed: 100, end: 140, want: loyaltyrepo.DebitResult{Balance: debitBalance("viewer", 40), Spent: true}},
		{name: "insufficient", seed: 20, end: 120, want: loyaltyrepo.DebitResult{Balance: debitBalance("original", 20)}},
		{name: "missing", end: 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := sqliteRetentionRepo(t)
			ctx := t.Context()
			require.NoError(t, r.EnsureBalanceCreditSchema(ctx))
			if tc.seed != 0 {
				adjustDebitBalance(t, r, tc.seed)
			}
			debit := loyaltyrepo.BalanceDebit{UserID: 42, ViewerID: 7, ViewerLogin: "@Viewer", Value: 60, Key: "recipe:one:1"}
			first, err := r.BalanceDebit(ctx, debit)
			require.NoError(t, err)
			require.Equal(t, tc.want, first)

			adjustDebitBalance(t, r, 100)
			wantReplay := tc.want
			wantReplay.Replayed = true
			debit.ViewerLogin = "renamed"
			replay, err := r.BalanceDebit(ctx, debit)
			require.NoError(t, err)
			require.Equal(t, wantReplay, replay)

			debit.Value++
			_, err = r.BalanceDebit(ctx, debit)
			require.ErrorIs(t, err, loyaltyrepo.ErrCreditKeyMismatch)
			row, _, err := r.BalanceGet(ctx, 42, 7)
			require.NoError(t, err)
			require.Equal(t, tc.end, row.Points)
		})
	}
}

func TestBalanceCreditAndDebitUseDistinctReceipts(t *testing.T) {
	r, _ := sqliteRetentionRepo(t)
	ctx := t.Context()
	require.NoError(t, r.EnsureBalanceCreditSchema(ctx))
	mutation := loyaltyrepo.BalanceMutation{UserID: 42, ViewerID: 7, ViewerLogin: "viewer", Value: 60, Key: "recipe:one:1"}
	for _, tc := range []struct {
		name   string
		mutate func() (loyaltyrepo.BalanceMutationResult, error)
		want   loyaltyrepo.BalanceMutationResult
	}{
		{name: "credit", mutate: func() (loyaltyrepo.BalanceMutationResult, error) { return r.BalanceCredit(ctx, mutation) },
			want: loyaltyrepo.CreditResult{Balance: debitBalance("viewer", 60)}},
		{name: "debit", mutate: func() (loyaltyrepo.BalanceMutationResult, error) { return r.BalanceDebit(ctx, mutation) },
			want: loyaltyrepo.DebitResult{Balance: debitBalance("viewer", 0), Spent: true}},
		{name: "credit replay", mutate: func() (loyaltyrepo.BalanceMutationResult, error) { return r.BalanceCredit(ctx, mutation) },
			want: loyaltyrepo.CreditResult{Balance: debitBalance("viewer", 60), Replayed: true}},
		{name: "debit replay", mutate: func() (loyaltyrepo.BalanceMutationResult, error) { return r.BalanceDebit(ctx, mutation) },
			want: loyaltyrepo.DebitResult{Balance: debitBalance("viewer", 0), Spent: true, Replayed: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.mutate()
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func adjustDebitBalance(t *testing.T, r *loyaltyrepo.Loyalty, points int64) {
	t.Helper()
	_, _, err := r.BalanceAdjustViewer(t.Context(), loyaltyrepo.BalanceAdjustment{UserID: 42, ViewerID: 7, ViewerLogin: "original", Value: points})
	require.NoError(t, err)
}

func debitBalance(login string, points int64) *loyaltyrpc.Balance {
	return &loyaltyrpc.Balance{ViewerID: "7", ViewerLogin: login, Points: points, PointsExact: strconv.FormatInt(points, 10)}
}
