// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"
	"github.com/go-sql-driver/mysql"
)

type earnedOperation struct {
	event    data.LoyaltyEarnedDTO
	identity string
	digest   string
}

func (r *Loyalty) ApplyEarned(ctx context.Context, dto data.LoyaltyEarnedDTO) error {
	if dto.BatchID == "" || dto.UserID == 0 {
		return fmt.Errorf("%w: earned identity", ErrInvalidInput)
	}
	ctx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()
	body, err := codec.Marshal(dto)
	if err != nil {
		return err
	}
	identity := sha256.Sum256([]byte(dto.BatchID))
	digest := sha256.Sum256(body)
	operation := earnedOperation{event: dto, identity: hex.EncodeToString(identity[:]), digest: hex.EncodeToString(digest[:])}
	err = r.persistEarned(ctx, operation)
	if err == nil {
		r.publishEarned(dto.UserID, dto.Entries)
	}
	return err
}

func (r *Loyalty) persistEarned(ctx context.Context, operation earnedOperation) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		return retryTx(ctx, func(ctx context.Context) error { return r.commitEarned(ctx, operation) })
	})
}

func (r *Loyalty) commitEarned(ctx context.Context, operation earnedOperation) error {
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := r.applyEarnedDelivery(ctx, tx, operation); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Loyalty) applyEarnedDelivery(ctx context.Context, tx *sql.Tx, operation earnedOperation) error {
	deleted, err := r.earnedFence(ctx, tx, operation.event.UserID)
	if err != nil {
		return err
	}
	added, err := r.recordEarnedReceipt(ctx, tx, operation)
	if err != nil {
		return err
	}
	if deleted || !added {
		return nil
	}
	return r.writeEarnedTransaction(ctx, tx, operation.event)
}

func (r *Loyalty) earnedFence(ctx context.Context, tx *sql.Tx, id uint64) (bool, error) {
	if r.dialect == "sqlite3" {
		account, err := r.fence(ctx, tx, id, 0)
		return account.deleted, err
	}
	fences, err := r.lockCounterUsers(ctx, tx, []uint64{id})
	if err != nil {
		return false, err
	}
	fence, found := fences[id]
	if !found {
		return false, sql.ErrNoRows
	}
	return fence.retired, nil
}

func (r *Loyalty) recordEarnedReceipt(ctx context.Context, tx *sql.Tx, operation earnedOperation) (bool, error) {
	suffix := " ON DUPLICATE KEY UPDATE batch_id=batch_id"
	if r.dialect == "sqlite3" {
		suffix = " ON CONFLICT(batch_id) DO NOTHING"
	}
	stmt := "INSERT INTO loyalty_earned_receipts (batch_id,payload_hash) VALUES (?, ?)" + suffix
	result, err := tx.ExecContext(ctx, stmt, operation.identity, operation.digest)
	if err != nil {
		return false, err
	}
	inserted, err := result.RowsAffected()
	if err != nil || inserted != 0 {
		return inserted != 0, err
	}
	return false, operation.checkReceipt(ctx, tx)
}

func (operation earnedOperation) checkReceipt(ctx context.Context, tx *sql.Tx) error {
	var previous string
	if err := tx.QueryRowContext(ctx, "SELECT payload_hash FROM loyalty_earned_receipts WHERE batch_id=?", operation.identity).Scan(&previous); err != nil {
		return err
	}
	if previous != operation.digest {
		return fmt.Errorf("%w: earned batch payload mismatch", ErrInvalidInput)
	}
	return nil
}

type earnedWindow struct {
	user uint64
	sums map[balKey]*earnSum
}

func (r *Loyalty) writeEarnedTransaction(ctx context.Context, tx *sql.Tx, dto data.LoyaltyEarnedDTO) error {
	sums, err := collectEarned(dto)
	if err != nil {
		return err
	}
	window := earnedWindow{user: dto.UserID, sums: sums}
	ids := make([]uint64, 0, len(sums))
	for key := range sums {
		ids = append(ids, key.viewerID)
	}
	slices.Sort(ids)
	for start := 0; start < len(ids); start += upsertChunk {
		chunk := ids[start:min(start+upsertChunk, len(ids))]
		if err := r.writeEarnedChunk(ctx, tx, window, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (r *Loyalty) writeEarnedChunk(ctx context.Context, tx *sql.Tx, window earnedWindow, ids []uint64) error {
	if err := window.checkChunk(ctx, tx, ids); err != nil {
		return err
	}
	stmt := r.earnedChunk(window, ids)
	_, err := tx.ExecContext(ctx, stmt.sql, stmt.args...)
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1690 {
		return fmt.Errorf("%w: watch balance overflow", ErrInvalidInput)
	}
	return err
}

func collectEarned(dto data.LoyaltyEarnedDTO) (map[balKey]*earnSum, error) {
	window := &Loyalty{earnPend: map[balKey]*earnSum{}}
	for _, e := range dto.Entries {
		if !earnsAnything(e) {
			continue
		}
		key := balKey{dto.UserID, e.ViewerID}
		if old := window.earnPend[key]; old != nil && old.watchSeconds > math.MaxUint64-e.WatchSeconds {
			return nil, fmt.Errorf("%w: earned watch sum overflow", ErrInvalidInput)
		}
		window.foldEarn(key, e)
	}
	return window.earnPend, nil
}

func (window earnedWindow) checkChunk(ctx context.Context, tx *sql.Tx, ids []uint64) error {
	args := []any{window.user}
	marks := make([]string, len(ids))
	for i, id := range ids {
		args = append(args, id)
		marks[i] = "?"
	}
	rows, err := tx.QueryContext(ctx, "SELECT viewer_id,watch_seconds FROM balances WHERE user_id=? AND viewer_id IN ("+strings.Join(marks, ",")+")", args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	return window.checkRows(rows)
}

func (window earnedWindow) checkRows(rows *sql.Rows) error {
	for rows.Next() {
		var id, seconds uint64
		if err := rows.Scan(&id, &seconds); err != nil {
			return err
		}
		if seconds > math.MaxUint64-window.sums[balKey{window.user, id}].watchSeconds {
			return fmt.Errorf("%w: watch balance overflow", ErrInvalidInput)
		}
	}
	return rows.Err()
}

func (r *Loyalty) earnedChunk(window earnedWindow, ids []uint64) chunkStmt {
	now := time.Now()
	rows := make([][]any, 0, len(ids))
	for _, id := range ids {
		e := window.sums[balKey{window.user, id}]
		rows = append(rows, []any{window.user, id, e.login, e.name, e.points, e.watchSeconds, now, now})
	}
	spec := upsertSpec{insert: "INSERT INTO balances (user_id,viewer_id,viewer_login,viewer_name,points,watch_seconds,created_at,updated_at) VALUES ", placeholder: "(?, ?, ?, ?, ?, ?, ?, ?)", suffix: r.earnedBalanceConflict()}
	return spec.render(rows)
}
