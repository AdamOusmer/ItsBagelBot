// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"database/sql"
	"slices"
	"testing"
	"time"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"github.com/stretchr/testify/require"
)

func TestWatchRetiredPartialAwardCompletesAndPrunes(t *testing.T) {
	for dialect, open := range map[string]func(*testing.T) (*loyaltyrepo.Loyalty, *sql.DB){
		"sqlite": sqliteRetentionRepo,
		"mysql":  mysqlRetentionRepo,
	} {
		t.Run(dialect, func(t *testing.T) {
			for _, recreate := range []bool{false, true} {
				t.Run(map[bool]string{false: "deleted", true: "recreated"}[recreate], func(t *testing.T) {
					repo, raw := open(t)
					checkRetiredWatchAward(t, watchBulkFixture{repo, raw}, watchAbort{dialect, 4}, recreate)
				})
			}
		})
	}
}

func checkRetiredWatchAward(t *testing.T, f watchBulkFixture, abort watchAbort, recreate bool) {
	t.Helper()
	old := time.Now().Add(-9 * 24 * time.Hour)
	a := bulkWatchAward(1, 1000)
	a.WindowStartedAtUnixMilli = old.UnixMilli()
	_, err := f.raw.ExecContext(t.Context(), abort.trigger())
	require.NoError(t, err)
	require.ErrorContains(t, f.repo.ApplyWatchAward(t.Context(), a), "chunk abort")
	require.Equal(t, watchTotals{192, 1920, 57600, 0, 192}, f.totals(t))
	_, err = f.raw.ExecContext(t.Context(), "DROP TRIGGER abort_watch")
	require.NoError(t, err)
	require.NoError(t, f.repo.DeleteAccount(t.Context(), a.UserID, a.AccountCreatedAt))
	if recreate {
		require.NoError(t, f.repo.RestoreUser(t.Context(), a.UserID, a.AccountCreatedAt+1))
	}
	changed := a
	changed.Entries = slices.Clone(a.Entries)
	changed.Entries[0].Points++
	require.ErrorContains(t, f.repo.ApplyWatchAward(t.Context(), changed), "payload mismatch")
	_, err = f.raw.ExecContext(t.Context(), (watchAbort{abort.dialect, 17}).trigger())
	require.NoError(t, err)
	require.ErrorContains(t, f.repo.ApplyWatchAward(t.Context(), a), "chunk abort")
	require.Equal(t, watchTotals{receipts: 192}, f.totals(t))
	_, err = f.raw.ExecContext(t.Context(), "DROP TRIGGER abort_watch")
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, f.repo.ApplyWatchAward(t.Context(), a))
		require.Equal(t, watchTotals{operations: 1, receipts: 192}, f.totals(t))
	}
	for _, table := range []string{"loyalty_watch_operations", "loyalty_watch_viewers"} {
		_, err := f.raw.ExecContext(t.Context(), "UPDATE "+table+" SET created_at=?", old.Unix())
		require.NoError(t, err)
	}
	require.NoError(t, f.repo.PruneWatchHistory(t.Context(), old.Add(24*time.Hour).UnixMilli()))
	for _, table := range []string{"loyalty_watch_operations", "loyalty_watch_viewers"} {
		var count int
		require.NoError(t, f.raw.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+table).Scan(&count))
		require.Zero(t, count)
	}
	require.NoError(t, f.repo.ApplyWatchAward(t.Context(), a))
	require.Equal(t, watchTotals{}, f.totals(t))
}
