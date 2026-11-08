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
)

type earnedOperation struct {
	event    data.LoyaltyEarnedDTO
	identity string
	digest   string
}

// ApplyEarned commits the receipt and credits atomically. Receipts deliberately
// have no expiry: an uncertain commit or a delayed broker replay must never
// award a balance twice. Legacy messages use their stable broker UUID.
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
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	err = db.WithExec(ctx, func(ctx context.Context) error {
		return retryTx(ctx, func(ctx context.Context) error {
			return r.commitEarned(ctx, operation)
		})
	})
	if err == nil {
		window := &Loyalty{earnPend: map[balKey]*earnSum{}}
		for _, e := range dto.Entries {
			window.foldEarn(balKey{dto.UserID, e.ViewerID}, e)
		}
		r.publishEarned(window.earnPend)
	}
	return err
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

// The account lock serializes receipt admission with retirement and restore.
func (r *Loyalty) applyEarnedDelivery(ctx context.Context, tx *sql.Tx, operation earnedOperation) error {
	_, deleted, err := r.fence(ctx, tx, operation.event.UserID, 0)
	if err != nil {
		return err
	}
	added, err := operation.recordReceipt(ctx, tx)
	if err != nil {
		return err
	}
	if deleted || !added {
		return nil
	}
	return r.writeEarnedTransaction(ctx, tx, operation.event)
}

func (operation earnedOperation) recordReceipt(ctx context.Context, tx *sql.Tx) (bool, error) {
	var previous string
	err := tx.QueryRowContext(ctx, "SELECT payload_hash FROM loyalty_earned_receipts WHERE batch_id=?", operation.identity).Scan(&previous)
	if err == nil {
		return false, verifyEarnedDigest(previous, operation.digest)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO loyalty_earned_receipts (batch_id,payload_hash) VALUES (?, ?)", operation.identity, operation.digest)
	return err == nil, err
}

func verifyEarnedDigest(previous, digest string) error {
	if previous != digest {
		return fmt.Errorf("%w: earned batch payload mismatch", ErrInvalidInput)
	}
	return nil
}

type earnedWindow struct {
	user uint64
	sums map[balKey]*earnSum
}

// Bound SQL work by chunks rather than issuing two statements per viewer.
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
		if err := window.checkChunk(ctx, tx, chunk); err != nil {
			return err
		}
		stmt := r.earnedChunk(window, chunk)
		if _, err := tx.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			return err
		}
	}
	return nil
}

func collectEarned(dto data.LoyaltyEarnedDTO) (map[balKey]*earnSum, error) {
	window := &Loyalty{earnPend: map[balKey]*earnSum{}}
	for _, e := range dto.Entries {
		if e.ViewerID == 0 {
			continue
		}
		if e.Points == 0 && e.WatchSeconds == 0 {
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
