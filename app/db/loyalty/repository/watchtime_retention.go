// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ItsBagelBot/internal/watchtime"
	"github.com/go-sql-driver/mysql"
)

func (r *Loyalty) ensureWatchWindowStartColumns(ctx context.Context) error {
	for _, table := range []string{"loyalty_watch_operations", "loyalty_watch_viewers"} {
		present := false
		if r.dialect == "sqlite3" {
			rows, err := r.sqldb.QueryContext(ctx, "PRAGMA table_info("+table+")")
			if err != nil {
				return err
			}
			for rows.Next() {
				var position, nonnull, primary int
				var name, kind string
				var defaultValue sql.NullString
				if err := rows.Scan(&position, &name, &kind, &nonnull, &defaultValue, &primary); err != nil {
					rows.Close()
					return err
				}
				present = present || name == "window_started_at_ms"
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
		} else {
			var n int
			if err := r.sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name='window_started_at_ms'`, table).Scan(&n); err != nil {
				return err
			}
			present = n != 0
		}
		if !present {
			if _, err := r.sqldb.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN window_started_at_ms BIGINT NOT NULL DEFAULT 0"); err != nil {
				var myErr *mysql.MySQLError
				if !errors.As(err, &myErr) || myErr.Number != 1060 {
					return err
				}
			}
		}
		name := "idx_" + table + "_window"
		if r.dialect == "sqlite3" {
			if _, err := r.sqldb.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS "+name+" ON "+table+" (window_started_at_ms,created_at)"); err != nil {
				return err
			}
		} else {
			var n int
			if err := r.sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name=? AND index_name=?`, table, name).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				if _, err := r.sqldb.ExecContext(ctx, "CREATE INDEX "+name+" ON "+table+" (window_started_at_ms,created_at)"); err != nil {
					var myErr *mysql.MySQLError
					if !errors.As(err, &myErr) || myErr.Number != 1061 {
						return err
					}
				}
			}
		}
	}
	return nil
}

const (
	watchHistoryPruneBatch  = 1000
	watchHistoryPrunePasses = 64
	watchHistoryPruneBudget = 2 * time.Second
)

func (r *Loyalty) ensureWatchRetentionSchema(ctx context.Context) error {
	if _, err := r.sqldb.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS loyalty_watch_retention (id INTEGER PRIMARY KEY, finalized_window_ms BIGINT NOT NULL)`); err != nil {
		return err
	}
	_, err := r.sqldb.ExecContext(ctx, r.insertIgnore()+` INTO loyalty_watch_retention (id,finalized_window_ms) VALUES (1,0)`)
	return err
}

// The retirement barrier is locked before any account fence or replay marker.
// A late replay cannot slip between advancing the barrier and pruning dedup.
func (r *Loyalty) lockWatchRetention(ctx context.Context, tx *sql.Tx) (int64, error) {
	if _, err := tx.ExecContext(ctx, `UPDATE loyalty_watch_retention SET finalized_window_ms=finalized_window_ms WHERE id=1`); err != nil {
		return 0, err
	}
	var finalized int64
	err := tx.QueryRowContext(ctx, `SELECT finalized_window_ms FROM loyalty_watch_retention WHERE id=1`).Scan(&finalized)
	return finalized, err
}

func (r *Loyalty) readWatchRetention(ctx context.Context, tx *sql.Tx) (int64, error) {
	if r.dialect == "sqlite3" {
		return r.lockWatchRetention(ctx, tx)
	}
	// A shared locking read admits unrelated tenants concurrently while an
	// advancing retirement barrier waits for all in-flight awards to finish.
	var finalized int64
	err := tx.QueryRowContext(ctx, `SELECT finalized_window_ms FROM loyalty_watch_retention WHERE id=1 LOCK IN SHARE MODE`).Scan(&finalized)
	return finalized, err
}

// PruneWatchHistory retires replay protection only after its source windows are
// permanently refused. The caller must first publish the same monotonic barrier
// at outbox admission and cap it below every retained, unpaid outbox window.
// Thus the seven-day policy bounds replay history without discarding accepted
// work while SQL is unavailable. Each invocation prunes bounded batches; a
// backlog is drained by subsequent maintenance passes.
func (r *Loyalty) PruneWatchHistory(ctx context.Context, safeCutoffUnixMilli int64) error {
	if safeCutoffUnixMilli <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, watchHistoryPruneBudget)
	defer cancel()
	limit := time.Now().Add(-watchtime.HistoryRetention).UnixMilli()
	if safeCutoffUnixMilli > limit {
		safeCutoffUnixMilli = limit
	}
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	finalized, err := r.lockWatchRetention(ctx, tx)
	if err != nil {
		return err
	}
	if safeCutoffUnixMilli > finalized {
		finalized = safeCutoffUnixMilli
		if _, err := tx.ExecContext(ctx, `UPDATE loyalty_watch_retention SET finalized_window_ms=? WHERE id=1`, finalized); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	// Cleanup does not hold the global barrier lock. Retired source windows
	// cannot write more markers, so each indexed batch can commit separately.
	for pass := 0; pass < watchHistoryPrunePasses && ctx.Err() == nil; pass++ {
		more := false
		for _, table := range []struct{ name, identity string }{{"loyalty_watch_operations", "operation_id"}, {"loyalty_watch_viewers", "award_id"}} {
			query := "DELETE FROM " + table.name + " WHERE created_at<=? AND window_started_at_ms<=? ORDER BY window_started_at_ms,created_at LIMIT ?"
			if r.dialect == "sqlite3" {
				query = "DELETE FROM " + table.name + " WHERE " + table.identity + " IN (SELECT " + table.identity + " FROM " + table.name + " WHERE created_at<=? AND window_started_at_ms<=? ORDER BY window_started_at_ms,created_at LIMIT ?)"
			}
			result, err := r.sqldb.ExecContext(ctx, query, finalized/1000, finalized, watchHistoryPruneBatch)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			more = more || n == watchHistoryPruneBatch
		}
		if !more {
			return nil
		}
	}
	return nil
}
