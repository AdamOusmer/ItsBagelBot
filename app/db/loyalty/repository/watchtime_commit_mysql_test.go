// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"os"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/require"
)

type watchBeginConnector struct {
	driver.Connector
	beforeBegin func() error
}

func (c *watchBeginConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	return watchBeginConn{Conn: conn, owner: c}, err
}

type watchBeginConn struct {
	driver.Conn
	owner *watchBeginConnector
}

func (c watchBeginConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if err := c.owner.interceptBegin(); err != nil {
		return nil, err
	}
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

func (c *watchBeginConnector) interceptBegin() error {
	if c.beforeBegin == nil {
		return nil
	}
	return c.beforeBegin()
}

func watchBeginHook(t *testing.T, raw *sql.DB) *watchBeginConnector {
	t.Helper()
	cfg, err := mysql.ParseDSN(os.Getenv("MYSQL_TEST_DSN"))
	require.NoError(t, err)
	require.NoError(t, raw.QueryRowContext(t.Context(), "SELECT DATABASE()").Scan(&cfg.DBName))
	inner, err := mysql.NewConnector(cfg)
	require.NoError(t, err)
	return &watchBeginConnector{Connector: inner}
}

func TestWatchMySQLErasureBetweenChunkCommits(t *testing.T) {
	for _, recreate := range []bool{false, true} {
		t.Run(map[bool]string{false: "deleted", true: "recreated"}[recreate], func(t *testing.T) {
			repo, raw := mysqlRetentionRepo(t)
			hook := watchBeginHook(t, raw)
			intercepted := sql.OpenDB(hook)
			t.Cleanup(func() { intercepted.Close() })
			posting := watchRetentionRepo(t, intercepted, "mysql")
			begins := 0
			hook.beforeBegin = func() error {
				begins++
				if begins != 5 {
					return nil
				}
				require.Equal(t, watchTotals{256, 2560, 76800, 0, 256}, (watchBulkFixture{repo, raw}).totals(t))
				require.NoError(t, repo.DeleteAccount(t.Context(), 17, 100))
				if recreate {
					return repo.RestoreUser(t.Context(), 17, 101)
				}
				return nil
			}
			a := bulkWatchAward(1, 1000)
			require.NoError(t, posting.ApplyWatchAward(t.Context(), a))
			require.Equal(t, 5, begins)
			require.Equal(t, watchTotals{operations: 1, receipts: 256}, (watchBulkFixture{repo, raw}).totals(t))
			hook.beforeBegin = nil
			require.NoError(t, posting.ApplyWatchAward(t.Context(), a))
			require.Equal(t, watchTotals{operations: 1, receipts: 256}, (watchBulkFixture{repo, raw}).totals(t))
		})
	}
}
