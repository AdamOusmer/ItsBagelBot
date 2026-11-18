// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/event/data"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const earnedFenceUser = 17

func TestEarnedMySQLConcurrentDeliveriesShareFence(t *testing.T) {
	repo, raw, ctx, tx := earnedMySQLFenceFixture(t)
	require.NoError(t, repo.ApplyEarned(ctx, earnedFenceDelivery("seed", 1)))
	var viewer uint64
	err := tx.QueryRowContext(ctx, "SELECT viewer_id FROM balances WHERE user_id=? AND viewer_id=1 FOR UPDATE", earnedFenceUser).Scan(&viewer)
	require.NoError(t, err)

	first := submitEarnedFenceDelivery(ctx, repo, earnedFenceDelivery("first", 1))
	waitForEarnedMySQLStatements(t, raw, "INSERT INTO balances%", 1)
	second := earnedFenceDelivery("second", 2)
	require.NoError(t, repo.ApplyEarned(ctx, second))
	require.NoError(t, tx.Commit())
	require.NoError(t, <-first)
	assertEarnedFenceTotals(t, raw, earnedFenceTotals{receipts: 3, balances: 2, points: 30})

	require.NoError(t, repo.ApplyEarned(ctx, second))
	second.Entries[0].Points++
	require.ErrorIs(t, repo.ApplyEarned(ctx, second), loyaltyrepo.ErrInvalidInput)
	assertEarnedFenceTotals(t, raw, earnedFenceTotals{receipts: 3, balances: 2, points: 30})
}

func TestEarnedMySQLDeliveriesWaitForRetirement(t *testing.T) {
	repo, raw, ctx, tx := earnedMySQLFenceFixture(t)
	require.NoError(t, repo.RestoreUser(ctx, earnedFenceUser, 100))
	_, err := tx.ExecContext(ctx, "UPDATE loyalty_account_fences SET deleted=1 WHERE user_id=?", earnedFenceUser)
	require.NoError(t, err)

	first := submitEarnedFenceDelivery(ctx, repo, earnedFenceDelivery("first", 1))
	second := submitEarnedFenceDelivery(ctx, repo, earnedFenceDelivery("second", 2))
	waitForEarnedMySQLStatements(t, raw, "SELECT%loyalty_account_fences%", 2)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-first)
	require.NoError(t, <-second)
	assertEarnedFenceTotals(t, raw, earnedFenceTotals{receipts: 2})
}

func TestEarnedMySQLConcurrentBalanceOverflowRollsBackReceipt(t *testing.T) {
	repo, raw, ctx, tx := earnedMySQLFenceFixture(t)
	require.NoError(t, repo.ApplyEarned(ctx, earnedFenceDelivery("seed", 1)))
	_, err := tx.ExecContext(ctx, "UPDATE balances SET watch_seconds=18446744073709551615 WHERE user_id=? AND viewer_id=1", earnedFenceUser)
	require.NoError(t, err)

	delivery := earnedFenceDelivery("overflow", 1)
	delivery.Entries[0].WatchSeconds = 1
	result := submitEarnedFenceDelivery(ctx, repo, delivery)
	waitForEarnedMySQLStatements(t, raw, "INSERT INTO balances%", 1)
	require.NoError(t, tx.Commit())
	require.ErrorIs(t, <-result, loyaltyrepo.ErrInvalidInput)
	assertEarnedFenceTotals(t, raw, earnedFenceTotals{receipts: 1, balances: 1, points: 10})
}

func earnedFenceDelivery(batchID string, viewer uint64) data.LoyaltyEarnedDTO {
	return data.LoyaltyEarnedDTO{
		BatchID: batchID,
		UserID:  earnedFenceUser,
		Entries: []data.LoyaltyEarnEntry{{ViewerID: viewer, Points: 10}},
	}
}

func earnedMySQLFenceFixture(t *testing.T) (*loyaltyrepo.Loyalty, *sql.DB, context.Context, *sql.Tx) {
	t.Helper()
	repo, raw := mysqlRetentionRepo(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	t.Cleanup(cancel)
	tx, err := raw.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	return repo, raw, ctx, tx
}

func submitEarnedFenceDelivery(ctx context.Context, repo *loyaltyrepo.Loyalty, dto data.LoyaltyEarnedDTO) <-chan error {
	done := make(chan error, 1)
	go func() { done <- repo.ApplyEarned(ctx, dto) }()
	return done
}

func waitForEarnedMySQLStatements(t *testing.T, raw *sql.DB, pattern string, count int) {
	t.Helper()
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var active int
		err := raw.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE DB=DATABASE() AND ID<>CONNECTION_ID() AND INFO LIKE ?", pattern).Scan(&active)
		assert.NoError(collect, err)
		assert.Equal(collect, count, active)
	}, 3*time.Second, 10*time.Millisecond)
}

type earnedFenceTotals struct{ receipts, balances, points int }

func assertEarnedFenceTotals(t *testing.T, raw *sql.DB, expected earnedFenceTotals) {
	t.Helper()
	var totals earnedFenceTotals
	err := raw.QueryRowContext(t.Context(), `SELECT (SELECT COUNT(*) FROM loyalty_earned_receipts),COUNT(*),COALESCE(SUM(points),0) FROM balances`).Scan(&totals.receipts, &totals.balances, &totals.points)
	require.NoError(t, err)
	require.Equal(t, expected, totals)
}
