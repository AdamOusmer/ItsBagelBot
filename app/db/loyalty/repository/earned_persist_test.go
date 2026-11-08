// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.
package repository_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"ItsBagelBot/app/db/loyalty/ent"
	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestEarnedFailureRollbackAndRestartReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "loyalty.db")
	raw, err := sql.Open(testdb.Driver, path+"?_fk=1")
	require.NoError(t, err)
	driver := entsql.OpenDB("sqlite3", raw)
	client := ent.NewClient(ent.Driver(driver))
	require.NoError(t, client.Schema.Create(t.Context()))
	r := loyaltyrepo.NewLoyalty(client, driver, nil, zap.NewNop())
	require.NoError(t, r.EnsureWatchSchema(t.Context()))
	dto := data.LoyaltyEarnedDTO{BatchID: "stable-earned-batch", UserID: 17, Entries: []data.LoyaltyEarnEntry{{ViewerID: 7, Points: 10}, {ViewerID: 8, Points: 20}}}
	for viewer := uint64(9); viewer <= 1006; viewer++ {
		dto.Entries = append(dto.Entries, data.LoyaltyEarnEntry{ViewerID: viewer, Points: 1})
	}
	// Fail after the first chunk has already been written: receipt and ALL
	// credits must roll back, rather than ACKing or retaining a partial award.
	_, err = raw.Exec(`CREATE TRIGGER fail_earned BEFORE INSERT ON balances WHEN NEW.viewer_id=1006 BEGIN SELECT RAISE(ABORT, 'injected failure'); END`)
	require.NoError(t, err)
	require.Error(t, r.ApplyEarned(t.Context(), dto))
	var n int
	require.NoError(t, raw.QueryRow("SELECT COUNT(*) FROM balances").Scan(&n))
	require.Zero(t, n)
	require.NoError(t, raw.QueryRow("SELECT COUNT(*) FROM loyalty_earned_receipts").Scan(&n))
	require.Zero(t, n)
	_, err = raw.Exec("DROP TRIGGER fail_earned")
	require.NoError(t, err)
	require.NoError(t, r.ApplyEarned(t.Context(), dto))
	r.Close(t.Context())
	require.NoError(t, raw.Close())
	raw2, err := sql.Open(testdb.Driver, path+"?_fk=1")
	require.NoError(t, err)
	t.Cleanup(func() { raw2.Close() })
	restarted := watchRetentionRepo(t, raw2, "sqlite3")
	// Represents a lost commit response/ACK followed by restart and redelivery.
	require.NoError(t, restarted.ApplyEarned(t.Context(), dto))
	for viewer, points := range map[uint64]int64{7: 10, 8: 20} {
		balance, found, err := restarted.BalanceGet(t.Context(), 17, viewer)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, points, balance.Points)
	}
	dto.Entries[0].Points++
	require.ErrorContains(t, restarted.ApplyEarned(t.Context(), dto), "payload mismatch")
}

func TestEarnedRetiredAccountDoesNotResurrect(t *testing.T) {
	r, _ := sqliteRetentionRepo(t)
	dto := data.LoyaltyEarnedDTO{BatchID: "old-earned", UserID: 17, Entries: []data.LoyaltyEarnEntry{{ViewerID: 7, Points: 10}}}
	require.NoError(t, r.ApplyEarned(t.Context(), dto))
	require.NoError(t, r.DeleteAccount(t.Context(), 17, 100))
	require.NoError(t, r.ApplyEarned(t.Context(), dto))
	dto.BatchID = "late-earned"
	require.NoError(t, r.ApplyEarned(t.Context(), dto))
	_, found, err := r.BalanceGet(t.Context(), 17, 7)
	require.NoError(t, err)
	require.False(t, found)
	require.NoError(t, r.RestoreUser(t.Context(), 17, 200))
	require.NoError(t, r.ApplyEarned(t.Context(), dto))
	_, found, err = r.BalanceGet(t.Context(), 17, 7)
	require.NoError(t, err)
	require.False(t, found, "a previously consumed batch cannot reappear after re-enrollment")
	dto.BatchID = "new-earned"
	require.NoError(t, r.ApplyEarned(t.Context(), dto))
	balance, found, err := r.BalanceGet(t.Context(), 17, 7)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 10, balance.Points)
}

func TestEarnedCancellationLeavesBatchRetryable(t *testing.T) {
	r, raw := sqliteRetentionRepo(t)
	dto := data.LoyaltyEarnedDTO{BatchID: "cancelled-earned", UserID: 17, Entries: []data.LoyaltyEarnEntry{{ViewerID: 7, Points: 10}}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, r.ApplyEarned(ctx, dto), context.Canceled)
	var n int
	require.NoError(t, raw.QueryRow("SELECT COUNT(*) FROM loyalty_earned_receipts").Scan(&n))
	require.Zero(t, n)
	require.NoError(t, r.ApplyEarned(t.Context(), dto))
	balance, found, err := r.BalanceGet(t.Context(), 17, 7)
	require.NoError(t, err)
	require.True(t, found)
	require.EqualValues(t, 10, balance.Points)
}
