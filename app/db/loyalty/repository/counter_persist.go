// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
	"github.com/go-sql-driver/mysql"
	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

// persistCounterBatch runs while persistMu is held. Every SQL error rolls back
// the complete coalesced transaction. Like the durable legacy writer, a trial
// promotion detected after writes restarts the rolled-back transaction with
// redirected totals. Only deadlocks and proven rolled-back promotion races
// are retried here; uncertain commit outcomes return to the handler.
func (r *Loyalty) persistCounterBatch(ctx context.Context, events []data.CounterBumpedDTO) error {
	txn := r.app.StartTransaction("persist counter batch")
	defer txn.End()
	ctx = newrelic.NewContext(ctx, txn)
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return retryTx(ctx, func(ctx context.Context) error { return r.commitCounterEvents(ctx, events) }, errTrialPromotedMidBatch)
	})
	if err != nil {
		txn.NoticeError(err)
	}
	return err
}

func (r *Loyalty) commitCounterEvents(ctx context.Context, events []data.CounterBumpedDTO) error {
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	users := make(map[uint64]bool)
	for _, event := range events {
		users[event.UserID] = false
	}
	ids := make([]uint64, 0, len(users))
	for id := range users {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		retired, err := r.counterBatchRetired(ctx, tx, id)
		if err != nil {
			return err
		}
		users[id] = retired
	}
	byUser := make(map[uint64]*Loyalty)
	for _, event := range events {
		if users[event.UserID] {
			continue
		}
		window := byUser[event.UserID]
		if window == nil {
			window = &Loyalty{bumpPend: make(map[bumpKey]*bumpSum)}
			byUser[event.UserID] = window
		}
		for _, bump := range event.Bumps {
			if err := window.collectBatchBump(event.UserID, bump); err != nil {
				return err
			}
		}
	}
	bumps := make(map[bumpKey]*bumpSum)
	unpromoted := make([]uint64, 0)
	for _, id := range ids {
		window := byUser[id]
		if window == nil {
			continue
		}
		trial := batchTrialBumps(id, window.bumpPend)
		if trial != (trialTotals{}) {
			promoted, err := trialPromoted(ctx, tx, readTrialPromotion, id)
			if err != nil {
				return err
			}
			if promoted {
				// Keep trial statistics and carry the same deltas to channel totals.
				// collectBatchBump validates overflow and applies Unicode/display rules.
				for _, carry := range trial.carried() {
					if err := window.collectBatchBump(id, data.CounterBumpEntry{Name: carry.name, Scope: data.CounterScopeChannel, Delta: carry.delta}); err != nil {
						return err
					}
				}
			} else {
				unpromoted = append(unpromoted, id)
			}
		}
		for key, sum := range window.bumpPend {
			bumps[key] = sum
		}
	}
	if err := r.ensureCounterBatchDefinitions(ctx, tx, bumps); err != nil {
		return err
	}
	if err := r.excludeSaturatedCounterRows(ctx, tx, bumps); err != nil {
		return err
	}
	if err := r.writeCounterSums(ctx, tx, bumps); err != nil {
		return err
	}
	// The recheck follows all trial row writes, exactly as writeCounterBatch:
	// promotion can commit after the snapshot read while we wait on its locks.
	for _, id := range unpromoted {
		query := lockTrialPromotion
		if r.dialect == "sqlite3" {
			query = readTrialPromotion
		}
		promoted, err := trialPromoted(ctx, tx, query, id)
		if err != nil {
			return err
		}
		if promoted {
			return errTrialPromotedMidBatch
		}
	}
	return tx.Commit()
}

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

func (r *Loyalty) writeCounterSums(ctx context.Context, tx *sql.Tx, bumps map[bumpKey]*bumpSum) error {
	keys := make([]bumpKey, 0, len(bumps))
	for key := range bumps {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return bumpKeyLess(keys[i], keys[j]) })
	now := time.Now()
	channel, entries := make([][]any, 0), make([][]any, 0)
	negatives := make([]bumpKey, 0)
	for _, key := range keys {
		sum := bumps[key]
		if sum.delta < 0 {
			negatives = append(negatives, key)
			continue
		}
		if entryScoped(sum.scope) {
			entries = append(entries, []any{key.userID, key.name, key.command, key.viewerID, sum.login, sum.name, sum.delta, now})
		} else {
			channel = append(channel, []any{key.userID, key.name, sum.scope, sum.delta, now, now})
		}
	}
	channelSuffix := " ON DUPLICATE KEY UPDATE value=value+VALUES(value), updated_at=VALUES(updated_at)"
	entrySuffix := " ON DUPLICATE KEY UPDATE value=value+VALUES(value), viewer_login=IF(VALUES(viewer_login)='',viewer_login,VALUES(viewer_login)), viewer_name=IF(VALUES(viewer_name)='',viewer_name,VALUES(viewer_name)), updated_at=VALUES(updated_at)"
	if r.dialect == "sqlite3" {
		channelSuffix = " ON CONFLICT(user_id,name) DO UPDATE SET value=value+excluded.value,updated_at=excluded.updated_at"
		entrySuffix = " ON CONFLICT(user_id,name,command,viewer_id) DO UPDATE SET value=value+excluded.value, viewer_login=CASE WHEN excluded.viewer_login='' THEN viewer_login ELSE excluded.viewer_login END, viewer_name=CASE WHEN excluded.viewer_name='' THEN viewer_name ELSE excluded.viewer_name END, updated_at=excluded.updated_at"
	}
	if err := writeCounterRows(ctx, tx, upsertSpec{label: "counters", insert: "INSERT INTO counters (user_id,name,scope,value,created_at,updated_at) VALUES ", placeholder: "(?, ?, ?, ?, ?, ?)", suffix: channelSuffix}, channel); err != nil {
		return err
	}
	if err := writeCounterRows(ctx, tx, upsertSpec{label: "counter entries", insert: "INSERT INTO counter_entries (user_id,name,command,viewer_id,viewer_login,viewer_name,value,updated_at) VALUES ", placeholder: "(?, ?, ?, ?, ?, ?, ?, ?)", suffix: entrySuffix}, entries); err != nil {
		return err
	}
	for _, key := range negatives {
		sum := bumps[key]
		query := "UPDATE counters SET value=CASE WHEN value+? < 0 THEN 0 ELSE value+? END,updated_at=? WHERE user_id=? AND name=?"
		args := []any{sum.delta, sum.delta, now, key.userID, key.name}
		if entryScoped(sum.scope) {
			identity := "viewer_login=IF(?='',viewer_login,?),viewer_name=IF(?='',viewer_name,?),"
			if r.dialect == "sqlite3" {
				identity = "viewer_login=CASE WHEN ?='' THEN viewer_login ELSE ? END,viewer_name=CASE WHEN ?='' THEN viewer_name ELSE ? END,"
			}
			query = "UPDATE counter_entries SET value=CASE WHEN value+? < 0 THEN 0 ELSE value+? END," + identity + "updated_at=? WHERE user_id=? AND name=? AND command=? AND viewer_id=?"
			args = []any{sum.delta, sum.delta, sum.login, sum.login, sum.name, sum.name, now, key.userID, key.name, key.command, key.viewerID}
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return counterWriteError(err)
		}
	}
	return nil
}

func counterWriteError(err error) error {
	var mysqlErr *mysql.MySQLError
	if (errors.As(err, &mysqlErr) && (mysqlErr.Number == 3819 || mysqlErr.Number == 1264 || mysqlErr.Number == 1690)) || sqliteCounterRangeError(err) {
		return fmt.Errorf("%w: counter value exceeds signed range: %v", ErrInvalidInput, err)
	}
	return err
}

// Saturation is a row-local permanent rejection. Read existing values in
// bounded queries under locks so healthy siblings from the same event commit.
// Definitions are already locked in the combined ascending table order.
func (r *Loyalty) excludeSaturatedCounterRows(ctx context.Context, tx *sql.Tx, bumps map[bumpKey]*bumpSum) error {
	channel, entries := splitBumps(bumps)
	for _, keys := range [][]bumpKey{channel, entries} {
		for start := 0; start < len(keys); start += upsertChunk {
			chunk := keys[start:min(start+upsertChunk, len(keys))]
			entry := entryScoped(bumps[chunk[0]].scope)
			table, columns, predicate, ordering := "counters", "user_id,name,value", "(user_id=? AND name=?)", "user_id,name"
			if entry {
				table, columns, predicate, ordering = "counter_entries", "user_id,name,command,viewer_id,value", "(user_id=? AND name=? AND command=? AND viewer_id=?)", "user_id,name,command,viewer_id"
			}
			predicates := make([]string, len(chunk))
			args := make([]any, 0, len(chunk)*4)
			for i, key := range chunk {
				predicates[i] = predicate
				args = append(args, key.userID, key.name)
				if entry {
					args = append(args, key.command, key.viewerID)
				}
			}
			query := "SELECT " + columns + " FROM " + table + " WHERE " + strings.Join(predicates, " OR ") + " ORDER BY " + ordering
			if r.dialect != "sqlite3" {
				query += " FOR UPDATE"
			}
			rows, err := tx.QueryContext(ctx, query, args...)
			if err != nil {
				return err
			}
			rejected := 0
			for rows.Next() {
				var key bumpKey
				var value int64
				if entry {
					err = rows.Scan(&key.userID, &key.name, &key.command, &key.viewerID, &value)
				} else {
					err = rows.Scan(&key.userID, &key.name, &value)
				}
				if err != nil {
					rows.Close()
					return err
				}
				if sum := bumps[key]; sum != nil && sum.delta > 0 && value > data.MaxCounter-sum.delta {
					delete(bumps, key)
					rejected++
				}
			}
			err = rows.Err()
			closeErr := rows.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if rejected > 0 && r.log != nil {
				r.log.Warn("loyalty: saturated counter rows rejected", zap.String("table", table), zap.Int("rows", rejected))
			}
		}
	}
	return nil
}
