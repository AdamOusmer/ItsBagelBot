// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWatchBulkMySQLStatementCount(t *testing.T) {
	for _, tc := range []struct {
		name       string
		textLength int
		statements int64
	}{
		{"short text", 0, 37},
		{"maximum text", 64, 37},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, raw := mysqlRetentionRepo(t)
			raw.SetMaxOpenConns(1)
			a := bulkWatchAward(1, 1000)
			for i := range a.Entries {
				a.Entries[i].ViewerLogin = strings.Repeat("'", tc.textLength)
				a.Entries[i].ViewerName = strings.Repeat("\\", tc.textLength)
			}
			before := watchStatementCount(t, raw)
			require.NoError(t, repo.ApplyWatchAward(t.Context(), a))
			after := watchStatementCount(t, raw)
			require.Equal(t, tc.statements, after-before)
		})
	}
}

func watchStatementCount(t *testing.T, raw *sql.DB) int64 {
	t.Helper()
	var count int64
	rows, err := raw.QueryContext(t.Context(), "SHOW SESSION STATUS WHERE Variable_name IN ('Com_select','Com_insert')")
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var name string
		var value int64
		require.NoError(t, rows.Scan(&name, &value))
		count += value
	}
	require.NoError(t, rows.Err())
	return count
}

func TestWatchBulkMySQLRetriesWholeTransaction(t *testing.T) {
	for _, code := range []int{1213, 1205} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			repo, raw := mysqlRetentionRepo(t)
			raw.SetMaxOpenConns(1)
			_, err := raw.ExecContext(t.Context(), "SET @watch_conflicts=1")
			require.NoError(t, err)
			trigger := fmt.Sprintf(`CREATE TRIGGER watch_conflict BEFORE INSERT ON balances FOR EACH ROW
BEGIN
IF NEW.viewer_id=1000 AND @watch_conflicts>0 THEN
SET @watch_conflicts=@watch_conflicts-1;
SIGNAL SQLSTATE '40001' SET MYSQL_ERRNO=%d, MESSAGE_TEXT='watch conflict';
END IF;
END`, code)
			_, err = raw.ExecContext(t.Context(), trigger)
			require.NoError(t, err)
			a := bulkWatchAward(1, 1000)
			require.NoError(t, repo.ApplyWatchAward(t.Context(), a))
			require.NoError(t, repo.ApplyWatchAward(t.Context(), a))
			fixture := watchBulkFixture{repo: repo, raw: raw}
			require.Equal(t, watchTotals{1000, 10000, 300000, 1, 1000}, fixture.totals(t))
			var remaining int
			require.NoError(t, raw.QueryRowContext(t.Context(), "SELECT @watch_conflicts").Scan(&remaining))
			require.Zero(t, remaining)
		})
	}
}
