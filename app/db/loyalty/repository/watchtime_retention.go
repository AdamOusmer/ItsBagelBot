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
	for _, table := range watchHistoryTables {
		if err := r.ensureWatchHistoryAgeSchema(ctx, table); err != nil {
			return err
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
	cutoff := min(safeCutoffUnixMilli, time.Now().Add(-watchtime.HistoryRetention).UnixMilli())
	finalized, err := r.advanceWatchRetention(ctx, cutoff)
	if err != nil {
		return err
	}
	// Cleanup runs after the barrier transaction releases its lock. Retired
	// source windows can no longer write markers, so batches commit separately.
	return r.pruneRetiredWatchHistory(ctx, finalized)
}

func (r *Loyalty) ensureWatchWindowStartColumn(ctx context.Context, table watchHistoryTable) error {
	present, err := r.watchWindowStartColumnPresent(ctx, table)
	if err != nil || present {
		return err
	}
	_, err = r.sqldb.ExecContext(ctx, "ALTER TABLE "+table.name+" ADD COLUMN window_started_at_ms BIGINT NOT NULL DEFAULT 0")
	return ignoreMySQLSchemaRace(err, 1060)
}

func (r *Loyalty) watchWindowStartColumnPresent(ctx context.Context, table watchHistoryTable) (bool, error) {
	if r.dialect == "sqlite3" {
		return r.sqliteWatchColumnPresent(ctx, table)
	}
	var count int
	err := r.sqldb.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name='window_started_at_ms'`, table.name).Scan(&count)
	return count != 0, err
}

func (r *Loyalty) sqliteWatchColumnPresent(ctx context.Context, table watchHistoryTable) (bool, error) {
	rows, err := r.sqldb.QueryContext(ctx, "PRAGMA table_info("+table.name+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	present := false
	for rows.Next() {
		matches, err := scanWatchWindowColumn(rows)
		if err != nil {
			return false, err
		}
		present = present || matches
	}
	return present, rows.Err()
}

// watchHistoryIndex describes a ledger access path used by migrations and pruning.
type watchHistoryIndex struct {
	suffix  string
	columns string
}

func (r *Loyalty) ensureWatchIndex(ctx context.Context, table watchHistoryTable, index watchHistoryIndex) error {
	name := "idx_" + table.name + "_" + index.suffix
	stmt := "CREATE INDEX " + name + " ON " + table.name + " (" + index.columns + ")"
	if r.dialect == "sqlite3" {
		_, err := r.sqldb.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS "+name+" ON "+table.name+" ("+index.columns+")")
		return err
	}
	var count int
	if err := r.sqldb.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name=? AND index_name=?", table.name, name).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	_, err := r.sqldb.ExecContext(ctx, stmt)
	return ignoreMySQLSchemaRace(err, 1061)
}

func ignoreMySQLSchemaRace(err error, number uint16) error {
	var myErr *mysql.MySQLError
	if !errors.As(err, &myErr) {
		return err
	}
	if myErr.Number == number {
		return nil
	}
	return err
}

func (r *Loyalty) advanceWatchRetention(ctx context.Context, cutoff int64) (int64, error) {
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	finalized, err := r.lockWatchRetention(ctx, tx)
	if err != nil {
		return 0, err
	}
	if cutoff > finalized {
		finalized = cutoff
		if _, err := tx.ExecContext(ctx, `UPDATE loyalty_watch_retention SET finalized_window_ms=? WHERE id=1`, finalized); err != nil {
			return 0, err
		}
	}
	err = tx.Commit()
	return finalized, err
}

func (r *Loyalty) pruneRetiredWatchHistory(ctx context.Context, finalized int64) error {
	for pass := 0; pass < watchHistoryPrunePasses && ctx.Err() == nil; pass++ {
		more, err := r.pruneWatchHistoryPass(ctx, finalized)
		if err != nil || !more {
			return err
		}
	}
	return nil
}

func (r *Loyalty) pruneWatchHistoryPass(ctx context.Context, finalized int64) (bool, error) {
	more := false
	for _, table := range watchHistoryTables {
		filled, err := r.pruneWatchTable(ctx, finalized, table)
		if err != nil {
			return false, err
		}
		more = more || filled
	}
	return more, nil
}

func (r *Loyalty) watchHistoryDeleteQuery(table watchHistoryTable) string {
	condition := " WHERE created_at<=? AND window_started_at_ms<=? ORDER BY window_started_at_ms,created_at LIMIT ?"
	if r.dialect == "sqlite3" {
		return "DELETE FROM " + table.name + " WHERE " + table.identity + " IN (SELECT " + table.identity + " FROM " + table.name + condition + ")"
	}
	return "DELETE FROM " + table.name + condition
}

func (r *Loyalty) ensureWatchHistoryAgeSchema(ctx context.Context, table watchHistoryTable) error {
	if err := r.ensureWatchWindowStartColumn(ctx, table); err != nil {
		return err
	}
	return r.ensureWatchIndex(ctx, table, watchHistoryIndex{suffix: "window", columns: "window_started_at_ms,created_at"})
}

func scanWatchWindowColumn(rows *sql.Rows) (bool, error) {
	var position, nonnull, primary int
	var name, kind string
	var defaultValue sql.NullString
	err := rows.Scan(&position, &name, &kind, &nonnull, &defaultValue, &primary)
	return name == "window_started_at_ms", err
}

func (r *Loyalty) pruneWatchTable(ctx context.Context, finalized int64, table watchHistoryTable) (bool, error) {
	result, err := r.sqldb.ExecContext(ctx, r.watchHistoryDeleteQuery(table), finalized/1000, finalized, watchHistoryPruneBatch)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == watchHistoryPruneBatch, err
}
