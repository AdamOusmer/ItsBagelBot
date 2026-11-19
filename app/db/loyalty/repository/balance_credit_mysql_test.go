// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"github.com/stretchr/testify/require"
)

func TestBalanceCreditMySQLConcurrentSameKeyCreditsOnce(t *testing.T) {
	r, _ := mysqlRetentionRepo(t)
	ctx := t.Context()
	require.NoError(t, r.EnsureBalanceCreditSchema(ctx))
	credit := loyaltyrepo.BalanceCredit{UserID: 42, ViewerID: 7, ViewerLogin: "winner", Value: 10, Key: "raffle:one:1", AccountCreatedAt: 11}
	require.NoError(t, r.RestoreUser(ctx, credit.UserID, credit.AccountCreatedAt))

	type outcome struct {
		result loyaltyrepo.CreditResult
		err    error
	}
	start := make(chan struct{})
	results := make(chan outcome, 2)
	for range cap(results) {
		go func() {
			<-start
			result, err := r.BalanceCredit(ctx, credit)
			results <- outcome{result, err}
		}()
	}
	close(start)
	first, second := <-results, <-results

	want := &loyaltyrpc.Balance{ViewerID: "7", ViewerLogin: "winner", Points: 10, PointsExact: "10"}
	require.ElementsMatch(t, []outcome{{result: loyaltyrepo.CreditResult{Balance: want}},
		{result: loyaltyrepo.CreditResult{Balance: want, Replayed: true}}}, []outcome{first, second})
	row, found, err := r.BalanceGet(ctx, credit.UserID, credit.ViewerID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, want, loyaltyrepo.BalanceView(row))
}
