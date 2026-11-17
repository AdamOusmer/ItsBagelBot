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
		{"short text", 0, 147},
		{"maximum text", 64, 147},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, raw := mysqlRetentionRepo(t)
			raw.SetMaxOpenConns(1)
			a := bulkWatchAward(1, 1000)
			for i := range a.Entries {
				a.Entries[i].ViewerLogin = strings.Repeat("'", tc.textLength)
				a.Entries[i].ViewerName = strings.Repeat("\\", tc.textLength)
			}
			before := watchStatementCounts(t, raw)
			require.NoError(t, repo.ApplyWatchAward(t.Context(), a))
			after := watchStatementCounts(t, raw)
			require.Equal(t, watchSQLCounts{tc.statements, 16}, after.since(before))
			require.NoError(t, repo.ApplyWatchAward(t.Context(), a))
			require.Equal(t, watchSQLCounts{1, 0}, watchStatementCounts(t, raw).since(after))
		})
	}
}

type watchSQLCounts struct{ statements, commits int64 }

func (c watchSQLCounts) since(before watchSQLCounts) watchSQLCounts {
	return watchSQLCounts{c.statements - before.statements, c.commits - before.commits}
}

func watchStatementCounts(t *testing.T, raw *sql.DB) watchSQLCounts {
	t.Helper()
	var counts watchSQLCounts
	rows, err := raw.QueryContext(t.Context(), "SHOW SESSION STATUS WHERE Variable_name IN ('Com_select','Com_insert','Com_update','Com_commit')")
	require.NoError(t, err)
	defer rows.Close()
	targets := map[string]*int64{"Com_select": &counts.statements, "Com_insert": &counts.statements, "Com_update": &counts.statements, "Com_commit": &counts.commits}
	for rows.Next() {
		var name string
		var value int64
		require.NoError(t, rows.Scan(&name, &value))
		*targets[name] += value
	}
	require.NoError(t, rows.Err())
	return counts
}

func TestWatchBulkMySQLRetriesWholeTransaction(t *testing.T) {
	for _, code := range []int{1213, 1205} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			repo, raw := mysqlRetentionRepo(t)
			raw.SetMaxOpenConns(1)
			seed := bulkWatchAward(900, 950)
			seed.Chunk = 1
			require.NoError(t, repo.ApplyWatchAward(t.Context(), seed))
			_, err := raw.ExecContext(t.Context(), "SET @watch_conflicts=1, @watch_first_attempts=0, @watch_last_attempts=0")
			require.NoError(t, err)
			trigger := fmt.Sprintf(`CREATE TRIGGER watch_conflict BEFORE INSERT ON balances FOR EACH ROW
BEGIN
IF NEW.viewer_id=1 THEN SET @watch_first_attempts=@watch_first_attempts+1; END IF;
IF NEW.viewer_id=961 THEN SET @watch_last_attempts=@watch_last_attempts+1; END IF;
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
			require.Equal(t, watchTotals{1000, 10000, 300000, 2, 1000}, fixture.totals(t))
			var attempts struct{ remaining, first, last int }
			require.NoError(t, raw.QueryRowContext(t.Context(), "SELECT @watch_conflicts,@watch_first_attempts,@watch_last_attempts").Scan(&attempts.remaining, &attempts.first, &attempts.last))
			require.Equal(t, struct{ remaining, first, last int }{0, 1, 2}, attempts)
		})
	}
}
