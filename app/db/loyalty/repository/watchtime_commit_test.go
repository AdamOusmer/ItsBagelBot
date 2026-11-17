// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"database/sql"
	"fmt"
	"testing"

	loyaltyrepo "ItsBagelBot/app/db/loyalty/repository"
	"github.com/stretchr/testify/require"
)

func TestWatchChunkAbortResumesCommittedViewers(t *testing.T) {
	for dialect, open := range map[string]func(*testing.T) (*loyaltyrepo.Loyalty, *sql.DB){
		"sqlite": sqliteRetentionRepo,
		"mysql":  mysqlRetentionRepo,
	} {
		t.Run(dialect, func(t *testing.T) {
			for _, chunk := range []int{1, 4, 16, 17} {
				t.Run(fmt.Sprint(chunk), func(t *testing.T) {
					repo, raw := open(t)
					checkWatchChunkRecovery(t, watchBulkFixture{repo, raw}, watchAbort{dialect, chunk})
				})
			}
		})
	}
}

type watchAbort struct {
	dialect string
	chunk   int
}

func (a watchAbort) trigger() string {
	target := fmt.Sprintf("INSERT ON balances FOR EACH ROW WHEN NEW.viewer_id=%d", (a.chunk-1)*64+1)
	if a.chunk == 17 {
		target = "UPDATE ON loyalty_watch_operations FOR EACH ROW"
	}
	if a.dialect == "sqlite" {
		return "CREATE TRIGGER abort_watch BEFORE " + target + " BEGIN SELECT RAISE(ABORT, 'chunk abort'); END"
	}
	condition := fmt.Sprintf("NEW.viewer_id=%d", (a.chunk-1)*64+1)
	table := "INSERT ON balances"
	if a.chunk == 17 {
		table, condition = "UPDATE ON loyalty_watch_operations", "TRUE"
	}
	return "CREATE TRIGGER abort_watch BEFORE " + table + " FOR EACH ROW BEGIN IF " + condition + " THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='chunk abort'; END IF; END"
}

func checkWatchChunkRecovery(t *testing.T, f watchBulkFixture, abort watchAbort) {
	t.Helper()
	_, err := f.raw.ExecContext(t.Context(), abort.trigger())
	require.NoError(t, err)
	a := bulkWatchAward(1, 1000)
	require.ErrorContains(t, f.repo.ApplyWatchAward(t.Context(), a), "chunk abort")
	committed := int64(min(abort.chunk-1, 15) * 64)
	require.Equal(t, watchTotals{committed, committed * 10, committed * 300, 0, committed}, f.totals(t))
	checkWatchIncompleteReplay(t, f, abort)
	_, err = f.raw.ExecContext(t.Context(), "DROP TRIGGER abort_watch")
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, f.repo.ApplyWatchAward(t.Context(), a))
		require.Equal(t, watchTotals{1000, 10000, 300000, 1, 1000}, f.totals(t))
	}
	var invalid int
	require.NoError(t, f.raw.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM balances WHERE points<>10 OR watch_seconds<>300").Scan(&invalid))
	require.Zero(t, invalid)
}

func checkWatchIncompleteReplay(t *testing.T, f watchBulkFixture, abort watchAbort) {
	t.Helper()
	if abort.chunk == 1 {
		return
	}
	var completed int
	require.NoError(t, f.raw.QueryRowContext(t.Context(), "SELECT completed FROM loyalty_watch_operations").Scan(&completed))
	require.Zero(t, completed)
	changed := bulkWatchAward(1, 1000)
	changed.Entries[0].Points++
	require.ErrorContains(t, f.repo.ApplyWatchAward(t.Context(), changed), "payload mismatch")
}
