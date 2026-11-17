// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"ItsBagelBot/pkg/db"
	"github.com/go-sql-driver/mysql"
)

func counterUpsertStatements(spec upsertSpec, rows [][]any) ([]chunkStmt, error) {
	chunks, err := db.StatementRowChunks(rows, len(spec.insert)+len(spec.suffix)+len(spec.placeholder)*db.StatementRows)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidInput, err)
	}
	stmts := make([]chunkStmt, 0, len(chunks))
	for _, chunk := range chunks {
		stmts = append(stmts, spec.render(chunk))
	}
	return stmts, nil
}

func writeCounterStatements(ctx context.Context, tx counterSQL, stmts []chunkStmt) error {
	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			return counterWriteError(err)
		}
	}
	return nil
}

type counterMutationRows struct {
	counters        [][]any
	entries         [][]any
	entryKeys       []bumpKey
	negatives       []bumpKey
	negativeEntries bool
}

type counterEffects struct {
	bumps      map[bumpKey]*bumpSum
	unpromoted []uint64
}

type counterWrites struct {
	rows     counterMutationRows
	counters []chunkStmt
	entries  []chunkStmt
}

func (r *Loyalty) writeCounterSums(ctx context.Context, tx counterTransaction, effects counterEffects) error {
	writes, err := r.prepareCounterWrites(effects.bumps)
	if err != nil {
		return err
	}
	if tx.mysql && writes.canPipeline(effects) {
		stmts := append(writes.counters, writes.entries...)
		packet := counterPacket(append(stmts, chunkStmt{sql: "COMMIT"}))
		return writeCounterStatements(ctx, tx, []chunkStmt{packet})
	}
	if err := r.writeCountersSequentially(ctx, tx, writes, effects.bumps); err != nil {
		return err
	}
	if err := verifyTrialPromotions(ctx, tx, effects.unpromoted); err != nil {
		return err
	}
	if tx.mysql {
		_, err := tx.ExecContext(ctx, "COMMIT")
		return err
	}
	return nil
}

func (w counterWrites) canPipeline(effects counterEffects) bool {
	singleChunk := len(w.counters) <= 1 && len(w.entries) <= 1
	return singleChunk && len(w.rows.negatives) == 0 && len(effects.unpromoted) == 0
}

func (r *Loyalty) prepareCounterWrites(bumps map[bumpKey]*bumpSum) (counterWrites, error) {
	rows := counterRowsForMutation(bumps, time.Now())
	counters, err := counterUpsertStatements(counterChannelWriteSpec(r.dialect), rows.counters)
	if err != nil {
		return counterWrites{}, err
	}
	entries, err := counterUpsertStatements(counterEntryWriteSpec(r.dialect), rows.entries)
	return counterWrites{rows: rows, counters: counters, entries: entries}, err
}

func (r *Loyalty) writeCountersSequentially(ctx context.Context, tx counterSQL, writes counterWrites, bumps map[bumpKey]*bumpSum) error {
	if err := writeCounterStatements(ctx, tx, writes.counters); err != nil {
		return err
	}
	negatives, err := r.lockNegativeBatchEntries(ctx, tx, writes.rows)
	if err != nil {
		return err
	}
	stmts := writes.entries
	now := time.Now()
	for _, key := range negatives {
		stmts = append(stmts, counterNegativeStatement(r.dialect, key, bumps[key], now))
	}
	return writeCounterStatements(ctx, tx, stmts)
}

func counterRowsForMutation(bumps map[bumpKey]*bumpSum, now time.Time) counterMutationRows {
	var rows counterMutationRows
	var previous bumpKey
	for _, key := range slices.SortedFunc(maps.Keys(bumps), compareBumpKeys) {
		sum := bumps[key]
		if key.userID != previous.userID || key.name != previous.name {
			rows.counters = append(rows.counters, counterDefinitionRow(key, sum, now))
		}
		rows.add(key, sum, now)
		previous = key
	}
	return rows
}

func counterDefinitionRow(key bumpKey, sum *bumpSum, now time.Time) []any {
	delta := int64(0)
	if !entryScoped(sum.scope) {
		delta = max(sum.delta, 0)
	}
	return []any{key.userID, key.name, sum.scope, delta, now, now}
}

func (r *counterMutationRows) add(key bumpKey, sum *bumpSum, now time.Time) {
	if sum.delta < 0 {
		r.negatives = append(r.negatives, key)
	}
	if !entryScoped(sum.scope) {
		return
	}
	r.entryKeys = append(r.entryKeys, key)
	if sum.delta < 0 {
		r.negativeEntries = true
		return
	}
	r.entries = append(r.entries, []any{key.userID, key.name, key.command, key.viewerID, sum.login, sum.name, sum.delta, now})
}

func counterChannelWriteSpec(dialect string) upsertSpec {
	const saturated = "counters.value > 9223372036854775807 - n.value"
	suffix := " AS n ON DUPLICATE KEY UPDATE " +
		"updated_at=IF(n.value=0 OR " + saturated + ",counters.updated_at,n.updated_at)," +
		"value=IF(" + saturated + ",counters.value,counters.value+n.value)"
	if dialect == "sqlite3" {
		const saturated = "value > 9223372036854775807 - excluded.value"
		suffix = " ON CONFLICT(user_id,name) DO UPDATE SET " +
			"updated_at=CASE WHEN excluded.value=0 OR " + saturated + " THEN updated_at ELSE excluded.updated_at END," +
			"value=CASE WHEN " + saturated + " THEN value ELSE value+excluded.value END"
	}
	return upsertSpec{label: "counters", insert: "INSERT INTO counters (user_id,name,scope,value,created_at,updated_at) VALUES ", placeholder: "(?, ?, ?, ?, ?, ?)", suffix: suffix}
}

func counterEntryWriteSpec(dialect string) upsertSpec {
	const saturated = "counter_entries.value > 9223372036854775807 - n.value"
	suffix := " AS n ON DUPLICATE KEY UPDATE " +
		"viewer_login=IF(n.viewer_login='' OR " + saturated + ",counter_entries.viewer_login,n.viewer_login)," +
		"viewer_name=IF(n.viewer_name='' OR " + saturated + ",counter_entries.viewer_name,n.viewer_name)," +
		"updated_at=IF(" + saturated + ",counter_entries.updated_at,n.updated_at)," +
		"value=IF(" + saturated + ",counter_entries.value,counter_entries.value+n.value)"
	if dialect == "sqlite3" {
		suffix = " ON CONFLICT(user_id,name,command,viewer_id) DO UPDATE SET " +
			"viewer_login=CASE WHEN excluded.viewer_login='' THEN viewer_login ELSE excluded.viewer_login END," +
			"viewer_name=CASE WHEN excluded.viewer_name='' THEN viewer_name ELSE excluded.viewer_name END," +
			"updated_at=excluded.updated_at,value=value+excluded.value WHERE value <= 9223372036854775807 - excluded.value"
	}
	return upsertSpec{label: "counter entries", insert: "INSERT INTO counter_entries (user_id,name,command,viewer_id,viewer_login,viewer_name,value,updated_at) VALUES ", placeholder: "(?, ?, ?, ?, ?, ?, ?, ?)", suffix: suffix}
}

const counterEntryLockPredicate = "(user_id=? AND name=? AND command=? AND viewer_id=?) OR "

func (r *Loyalty) lockNegativeBatchEntries(ctx context.Context, tx counterSQL, rows counterMutationRows) ([]bumpKey, error) {
	if !rows.negativeEntries || r.dialect == "sqlite3" {
		return rows.negatives, nil
	}
	keys := make([][]any, 0, len(rows.entryKeys))
	for _, key := range rows.entryKeys {
		keys = append(keys, []any{key.userID, key.name, key.command, key.viewerID})
	}
	chunks, err := db.StatementRowChunks(keys, len(counterEntryLockPredicate)*db.StatementRows+256)
	if err != nil {
		return nil, err
	}
	present := make(map[bumpKey]bool, len(keys))
	for _, chunk := range chunks {
		stmt := counterEntryLockStatement(chunk)
		if err := lockCounterEntryChunk(ctx, tx, stmt, present); err != nil {
			return nil, err
		}
	}
	return rows.lockedNegatives(present), nil
}

// An absent negative bucket takes no lock, so updating it after the positive upserts would break ascending lock order.
func (r counterMutationRows) lockedNegatives(present map[bumpKey]bool) []bumpKey {
	entries := make(map[bumpKey]bool, len(r.entryKeys))
	for _, key := range r.entryKeys {
		entries[key] = true
	}
	kept := make([]bumpKey, 0, len(r.negatives))
	for _, key := range r.negatives {
		if present[key] || !entries[key] {
			kept = append(kept, key)
		}
	}
	return kept
}

func counterEntryLockStatement(keys [][]any) chunkStmt {
	args := make([]any, 0, len(keys)*4)
	for _, key := range keys {
		args = append(args, key...)
	}
	where := strings.TrimSuffix(strings.Repeat(counterEntryLockPredicate, len(keys)), " OR ")
	query := "SELECT user_id,name,command,viewer_id FROM counter_entries FORCE INDEX (PRIMARY) WHERE " + where +
		" ORDER BY user_id,name,command,viewer_id FOR UPDATE"
	return chunkStmt{sql: query, args: args}
}

func lockCounterEntryChunk(ctx context.Context, tx counterSQL, stmt chunkStmt, present map[bumpKey]bool) error {
	rows, err := tx.QueryContext(ctx, stmt.sql, stmt.args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key bumpKey
		if err := rows.Scan(&key.userID, &key.name, &key.command, &key.viewerID); err != nil {
			return err
		}
		present[key] = true
	}
	return rows.Err()
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
