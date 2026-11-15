// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/internal/testdb"
	"github.com/stretchr/testify/require"
)

type counterSessionQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func counterSessionStatements(t *testing.T, conn counterSessionQuerier) map[string]int {
	t.Helper()
	rows, err := conn.QueryContext(t.Context(), "SHOW SESSION STATUS WHERE Variable_name IN ('Com_insert','Com_select')")
	require.NoError(t, err)
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		var name string
		var count int
		require.NoError(t, rows.Scan(&name, &count))
		counts[name] = count
	}
	require.NoError(t, rows.Err())
	return counts
}

func TestCounterMySQLBulkFenceStatementCount(t *testing.T) {
	raw := testdb.MySQL(t, "loyalty_counter_statements_")
	raw.SetMaxOpenConns(1)
	repo := mysqlReplica(t, raw)
	p := uncachedCounterProcessor(t, repo)
	held := holdCounterSQL(t, raw)
	first := make(chan error, 1)
	go func() { first <- p.Process(t.Context(), counterTestEvent("barrier", 1)) }()
	require.Eventually(t, func() bool { return len(p.slots) > 0 }, time.Second, time.Millisecond)
	time.Sleep(2 * counterBatchWindow)
	errs := make(chan error, counterBatchMessages)
	for i := range counterBatchMessages {
		go func() {
			event := counterTestEvent(fmt.Sprintf("bulk-fence-%d", i), 1)
			event.UserID = uint64(i + 2)
			errs <- p.Process(t.Context(), event)
		}()
	}
	require.Eventually(t, func() bool { return len(p.queue) == counterBatchMessages }, time.Second, time.Millisecond)
	before := counterSessionStatements(t, held)
	require.NoError(t, held.Close())
	require.NoError(t, <-first)
	for range counterBatchMessages {
		require.NoError(t, <-errs)
	}
	after := counterSessionStatements(t, raw)
	for name, expected := range map[string]int{"Com_insert": 8, "Com_select": 6} {
		require.Equal(t, expected, after[name]-before[name], name)
	}
	assertCounterReceipts(t, raw, counterBatchMessages+1)
}
