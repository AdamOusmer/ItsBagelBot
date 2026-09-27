// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"ItsBagelBot/internal/domain/event/data"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/go-sql-driver/mysql"
	"go.uber.org/zap"
	"sort"
	"strings"
	"time"
)

func bumpKeyLess(a, b bumpKey) bool { return compareBumpKeys(a, b) < 0 }

// Create/lock every definition in the upstream combined natural-key order.
// Separating entry definitions from channel rows would take the same table's
// locks in opposite orders during promotion or another service instance.
func (r *Loyalty) ensureCounterBatchDefinitions(ctx context.Context, tx *sql.Tx, bumps map[bumpKey]*bumpSum) error {
	channel, entries := splitBumps(bumps)
	now := time.Now()
	ordered := orderedCounterRows(channel, entries, bumps, now)
	rows := make([][]any, 0, len(ordered))
	for _, row := range ordered {
		rows = append(rows, []any{row.userID, row.name, row.args[2], now, now})
	}
	suffix := " ON DUPLICATE KEY UPDATE name=name"
	if r.dialect == "sqlite3" {
		suffix = " ON CONFLICT(user_id,name) DO NOTHING"
	}
	return writeCounterRows(ctx, tx, upsertSpec{label: "counter definitions", insert: "INSERT INTO counters (user_id,name,scope,value,created_at,updated_at) VALUES ", placeholder: "(?, ?, ?, 0, ?, ?)", suffix: suffix}, rows)
}

func writeCounterRows(ctx context.Context, tx *sql.Tx, spec upsertSpec, rows [][]any) error {
	for start := 0; start < len(rows); start += upsertChunk {
		stmt := spec.render(rows[start:min(start+upsertChunk, len(rows))])
		if _, err := tx.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			return counterWriteError(err)
		}
	}
	return nil
}

type counterMutationRows struct {
	channel   [][]any
	entries   [][]any
	negatives []bumpKey
}

func (r *Loyalty) writeCounterSums(ctx context.Context, tx *sql.Tx, bumps map[bumpKey]*bumpSum) error {
	now := time.Now()
	rows := counterRowsForMutation(bumps, now)
	if err := writeCounterRows(ctx, tx, counterChannelWriteSpec(r.dialect), rows.channel); err != nil {
		return err
	}
	if err := writeCounterRows(ctx, tx, counterEntryWriteSpec(r.dialect), rows.entries); err != nil {
		return err
	}
	return r.writeNegativeCounterRows(ctx, tx, rows.negatives, bumps, now)
}

func counterRowsForMutation(bumps map[bumpKey]*bumpSum, now time.Time) counterMutationRows {
	rows := counterMutationRows{}
	for _, key := range sortedCounterKeys(bumps) {
		rows.add(key, bumps[key], now)
	}
	return rows
}

func sortedCounterKeys(bumps map[bumpKey]*bumpSum) []bumpKey {
	keys := make([]bumpKey, 0, len(bumps))
	for key := range bumps {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return bumpKeyLess(keys[i], keys[j]) })
	return keys
}

func (r *counterMutationRows) add(key bumpKey, sum *bumpSum, now time.Time) {
	if sum.delta < 0 {
		r.negatives = append(r.negatives, key)
		return
	}
	if entryScoped(sum.scope) {
		r.entries = append(r.entries, []any{key.userID, key.name, key.command, key.viewerID, sum.login, sum.name, sum.delta, now})
		return
	}
	r.channel = append(r.channel, []any{key.userID, key.name, sum.scope, sum.delta, now, now})
}

func counterChannelWriteSpec(dialect string) upsertSpec {
	suffix := " ON DUPLICATE KEY UPDATE value=value+VALUES(value), updated_at=VALUES(updated_at)"
	if dialect == "sqlite3" {
		suffix = " ON CONFLICT(user_id,name) DO UPDATE SET value=value+excluded.value,updated_at=excluded.updated_at"
	}
	return upsertSpec{label: "counters", insert: "INSERT INTO counters (user_id,name,scope,value,created_at,updated_at) VALUES ", placeholder: "(?, ?, ?, ?, ?, ?)", suffix: suffix}
}

func counterEntryWriteSpec(dialect string) upsertSpec {
	suffix := " ON DUPLICATE KEY UPDATE value=value+VALUES(value), viewer_login=IF(VALUES(viewer_login)='',viewer_login,VALUES(viewer_login)), viewer_name=IF(VALUES(viewer_name)='',viewer_name,VALUES(viewer_name)), updated_at=VALUES(updated_at)"
	if dialect == "sqlite3" {
		suffix = " ON CONFLICT(user_id,name,command,viewer_id) DO UPDATE SET value=value+excluded.value, viewer_login=CASE WHEN excluded.viewer_login='' THEN viewer_login ELSE excluded.viewer_login END, viewer_name=CASE WHEN excluded.viewer_name='' THEN viewer_name ELSE excluded.viewer_name END, updated_at=excluded.updated_at"
	}
	return upsertSpec{label: "counter entries", insert: "INSERT INTO counter_entries (user_id,name,command,viewer_id,viewer_login,viewer_name,value,updated_at) VALUES ", placeholder: "(?, ?, ?, ?, ?, ?, ?, ?)", suffix: suffix}
}

func (r *Loyalty) writeNegativeCounterRows(ctx context.Context, tx *sql.Tx, keys []bumpKey, bumps map[bumpKey]*bumpSum, now time.Time) error {
	for _, key := range keys {
		stmt := counterNegativeStatement(r.dialect, key, bumps[key], now)
		if _, err := tx.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			return counterWriteError(err)
		}
	}
	return nil
}

func counterNegativeStatement(dialect string, key bumpKey, sum *bumpSum, now time.Time) chunkStmt {
	if entryScoped(sum.scope) {
		return counterNegativeEntryStatement(dialect, key, sum, now)
	}
	return chunkStmt{
		sql:  "UPDATE counters SET value=CASE WHEN value+? < 0 THEN 0 ELSE value+? END,updated_at=? WHERE user_id=? AND name=?",
		args: []any{sum.delta, sum.delta, now, key.userID, key.name},
	}
}

func counterNegativeEntryStatement(dialect string, key bumpKey, sum *bumpSum, now time.Time) chunkStmt {
	identity := "viewer_login=IF(?='',viewer_login,?),viewer_name=IF(?='',viewer_name,?),"
	if dialect == "sqlite3" {
		identity = "viewer_login=CASE WHEN ?='' THEN viewer_login ELSE ? END,viewer_name=CASE WHEN ?='' THEN viewer_name ELSE ? END,"
	}
	return chunkStmt{
		sql:  "UPDATE counter_entries SET value=CASE WHEN value+? < 0 THEN 0 ELSE value+? END," + identity + "updated_at=? WHERE user_id=? AND name=? AND command=? AND viewer_id=?",
		args: []any{sum.delta, sum.delta, sum.login, sum.login, sum.name, sum.name, now, key.userID, key.name, key.command, key.viewerID},
	}
}

func counterWriteError(err error) error {
	if mysqlCounterRangeError(err) || sqliteCounterRangeError(err) {
		return fmt.Errorf("%w: counter value exceeds signed range: %v", ErrInvalidInput, err)
	}
	return err
}

func mysqlCounterRangeError(err error) bool {
	var mysqlErr *mysql.MySQLError
	if !errors.As(err, &mysqlErr) {
		return false
	}
	switch mysqlErr.Number {
	case 3819, 1264, 1690:
		return true
	default:
		return false
	}
}

// Saturation is a row-local permanent rejection. Read existing values in
// bounded queries under locks so healthy siblings from the same event commit.
// Definitions are already locked in the combined ascending table order.
func (r *Loyalty) excludeSaturatedCounterRows(ctx context.Context, tx *sql.Tx, bumps map[bumpKey]*bumpSum) error {
	channel, entries := splitBumps(bumps)
	if err := r.inspectCounterChunks(ctx, tx, channel, bumps, false); err != nil {
		return err
	}
	return r.inspectCounterChunks(ctx, tx, entries, bumps, true)
}

func (r *Loyalty) inspectCounterChunks(ctx context.Context, tx *sql.Tx, keys []bumpKey, bumps map[bumpKey]*bumpSum, entry bool) error {
	shape := counterInspectionShape(entry)
	for start := 0; start < len(keys); start += upsertChunk {
		chunk := keys[start:min(start+upsertChunk, len(keys))]
		rejected, err := r.inspectCounterChunk(ctx, tx, shape, chunk, bumps)
		if err != nil {
			return err
		}
		r.noticeSaturatedCounters(shape.table, rejected)
	}
	return nil
}

type counterInspection struct {
	table     string
	columns   string
	predicate string
	ordering  string
	entry     bool
}

func counterInspectionShape(entry bool) counterInspection {
	if entry {
		return counterInspection{table: "counter_entries", columns: "user_id,name,command,viewer_id,value", predicate: "(user_id=? AND name=? AND command=? AND viewer_id=?)", ordering: "user_id,name,command,viewer_id", entry: true}
	}
	return counterInspection{table: "counters", columns: "user_id,name,value", predicate: "(user_id=? AND name=?)", ordering: "user_id,name"}
}

func (s counterInspection) statement(dialect string, keys []bumpKey) chunkStmt {
	predicates := make([]string, len(keys))
	args := make([]any, 0, len(keys)*4)
	for i, key := range keys {
		predicates[i] = s.predicate
		args = append(args, key.userID, key.name)
		if s.entry {
			args = append(args, key.command, key.viewerID)
		}
	}
	query := "SELECT " + s.columns + " FROM " + s.table + " WHERE " + strings.Join(predicates, " OR ") + " ORDER BY " + s.ordering
	if dialect != "sqlite3" {
		query += " FOR UPDATE"
	}
	return chunkStmt{sql: query, args: args}
}

func (r *Loyalty) inspectCounterChunk(ctx context.Context, tx *sql.Tx, shape counterInspection, keys []bumpKey, bumps map[bumpKey]*bumpSum) (int, error) {
	stmt := shape.statement(r.dialect, keys)
	rows, err := tx.QueryContext(ctx, stmt.sql, stmt.args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	rejected, err := shape.excludeSaturated(rows, bumps)
	if err != nil {
		return 0, err
	}
	return rejected, rows.Close()
}

func (s counterInspection) excludeSaturated(rows *sql.Rows, bumps map[bumpKey]*bumpSum) (int, error) {
	rejected := 0
	for rows.Next() {
		key, value, err := s.decode(rows)
		if err != nil {
			return 0, err
		}
		if counterSumSaturated(bumps[key], value) {
			delete(bumps, key)
			rejected++
		}
	}
	return rejected, rows.Err()
}

func (s counterInspection) decode(rows *sql.Rows) (bumpKey, int64, error) {
	var key bumpKey
	var value int64
	var err error
	if s.entry {
		err = rows.Scan(&key.userID, &key.name, &key.command, &key.viewerID, &value)
	} else {
		err = rows.Scan(&key.userID, &key.name, &value)
	}
	return key, value, err
}

func counterSumSaturated(sum *bumpSum, value int64) bool {
	if sum == nil {
		return false
	}
	if sum.delta <= 0 {
		return false
	}
	return value > data.MaxCounter-sum.delta
}

func (r *Loyalty) noticeSaturatedCounters(table string, rejected int) {
	if rejected == 0 {
		return
	}
	if r.log == nil {
		return
	}
	r.log.Warn("loyalty: saturated counter rows rejected", zap.String("table", table), zap.Int("rows", rejected))
}
