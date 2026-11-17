// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db_test

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"

	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/db"
	"github.com/stretchr/testify/require"
)

type sqlTransactionStarter struct{ *sql.DB }

func (s sqlTransactionStarter) Tx(ctx context.Context) (*sql.Tx, error) {
	return s.BeginTx(ctx, nil)
}

func TestRetryWithTxMySQLRollsBackEveryStatement(t *testing.T) {
	raw := testdb.MySQL(t, "db_retry_")
	_, err := raw.Exec("CREATE TABLE retry_rows (id INT PRIMARY KEY, value INT NOT NULL)")
	require.NoError(t, err)
	_, err = raw.Exec("INSERT INTO retry_rows VALUES (1,0),(2,0)")
	require.NoError(t, err)
	var ready sync.WaitGroup
	ready.Add(2)
	var attempts atomic.Int32
	errs := make(chan error, 2)
	for _, id := range []int{1, 2} {
		go func() {
			first := true
			errs <- db.RetryWithTx(t.Context(), sqlTransactionStarter{raw}, func(tx *sql.Tx) error {
				attempts.Add(1)
				if _, err := tx.ExecContext(t.Context(), "UPDATE retry_rows SET value=value+1 WHERE id=?", id); err != nil {
					return err
				}
				if first {
					first = false
					ready.Done()
					ready.Wait()
				}
				_, err := tx.ExecContext(t.Context(), "UPDATE retry_rows SET value=value+1 WHERE id=?", 3-id)
				return err
			})
		}()
	}
	for range 2 {
		require.NoError(t, <-errs)
	}
	var minValue, maxValue int
	require.NoError(t, raw.QueryRow("SELECT MIN(value), MAX(value) FROM retry_rows").Scan(&minValue, &maxValue))
	require.Equal(t, [2]int{2, 2}, [2]int{minValue, maxValue})
	require.GreaterOrEqual(t, attempts.Load(), int32(3))
}
