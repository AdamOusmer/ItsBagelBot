// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"testing"
	"time"

	"ItsBagelBot/pkg/db"
	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

func TestWatchMySQLRetryReleasesGate(t *testing.T) {
	if os.Getenv("MYSQL_TEST_DSN") == "" {
		t.Skip("set MYSQL_TEST_DSN to run isolated real-MySQL tests")
	}
	if os.Getenv("WATCH_GATE_TEST_CHILD") == "1" {
		checkWatchRetryGate(t)
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestWatchMySQLRetryReleasesGate$", "-test.timeout=20s")
	cmd.Env = append(os.Environ(), "WATCH_GATE_TEST_CHILD=1", "DB_QUERY_CONCURRENCY=1")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)
}

func checkWatchRetryGate(t *testing.T) {
	t.Helper()
	_, raw := mysqlRetentionRepo(t)
	hook := watchBeginHook(t, raw)
	intercepted := sql.OpenDB(hook)
	t.Cleanup(func() { intercepted.Close() })
	posting := watchRetentionRepo(t, intercepted, "mysql")
	available := make(chan error, 1)
	attempts := 0
	hook.beforeBegin = func() error {
		attempts++
		if attempts == 1 {
			go func() {
				available <- db.WithExec(t.Context(), func(context.Context) error { return nil })
			}()
			return &mysql.MySQLError{Number: db.MySQLDeadlock}
		}
		select {
		case err := <-available:
			return err
		case <-time.After(time.Second):
			return context.DeadlineExceeded
		}
	}
	require.NoError(t, posting.ApplyWatchAward(t.Context(), watchAward(17, 1, 100)))
	require.Equal(t, 2, attempts)
}
