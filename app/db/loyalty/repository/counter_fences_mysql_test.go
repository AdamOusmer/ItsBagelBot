// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/internal/testdb"
	"github.com/stretchr/testify/assert"
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
	require.NoError(t, repo.RestoreUser(t.Context(), 1, 1))
	held := holdCounterSQL(t, raw)
	first := make(chan error, 1)
	barrier := counterTestEvent("barrier", 1)
	barrier.Bumps = append(barrier.Bumps, replicaEntryBump(7, 1))
	go func() { first <- p.Process(t.Context(), barrier) }()
	require.Eventually(t, func() bool { return len(p.slots) > 0 }, time.Second, time.Millisecond)
	time.Sleep(2 * counterBatchWindow)
	errs := make(chan error, counterBatchMessages)
	for i := range counterBatchMessages {
		go func() {
			event := counterTestEvent(fmt.Sprintf("bulk-fence-%d", i), 1)
			event.UserID = uint64(i + 2)
			event.Bumps[0].Name = data.CounterTrialDecoded
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
	for name, expected := range map[string]int{"Com_insert": 6, "Com_select": 4} {
		require.Equal(t, expected, after[name]-before[name], name)
	}
	assertCounterReceipts(t, raw, counterBatchMessages+1)
}

func counterMySQLFenceFixture(t *testing.T) (*CounterProcessor, *sql.DB, *sql.Tx) {
	t.Helper()
	ctx := t.Context()
	raw := testdb.MySQL(t, "loyalty_counter_fence_")
	repo := mysqlReplica(t, raw)
	p := uncachedCounterProcessor(t, repo)
	require.NoError(t, repo.RestoreUser(ctx, 7, 1))
	tx, err := raw.BeginTx(ctx, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback() })
	return p, raw, tx
}

func TestCounterMySQLBatchSharesFenceWithAnotherBatch(t *testing.T) {
	p, raw, tx := counterMySQLFenceFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	var id uint64
	require.NoError(t, tx.QueryRowContext(ctx, "SELECT user_id FROM loyalty_account_fences WHERE user_id=7 FOR SHARE").Scan(&id))
	event := counterTestEvent("shared-fence", 1)
	event.UserID = id
	require.NoError(t, p.Process(ctx, event))

	var value int64
	err := raw.QueryRowContext(ctx, "SELECT value FROM counters WHERE user_id=7 AND name='deaths'").Scan(&value)
	require.NoError(t, err)
	require.EqualValues(t, 1, value)
}

func TestCounterMySQLBatchWaitsForFenceRetirement(t *testing.T) {
	p, raw, tx := counterMySQLFenceFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := tx.ExecContext(ctx, "UPDATE loyalty_account_fences SET deleted=1 WHERE user_id=7")
	require.NoError(t, err)
	event := counterTestEvent("retiring-fence", 1)
	event.UserID = 7
	result := make(chan error, 1)
	go func() { result <- p.Process(ctx, event) }()
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		var waiting bool
		err := raw.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.PROCESSLIST WHERE DB=DATABASE() AND ID<>CONNECTION_ID() AND INFO LIKE '%loyalty_account_fences%')").Scan(&waiting)
		assert.NoError(collect, err)
		assert.True(collect, waiting)
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, tx.Commit())
	require.NoError(t, <-result)

	var count int
	err = raw.QueryRowContext(ctx, "SELECT COUNT(*) FROM counters WHERE user_id=7").Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count)
}
