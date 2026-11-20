// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/db"
)

type SnapshotID string

type FreezeRequest struct {
	UserID  uint64
	Minutes int
}

type FreezeState struct {
	SnapshotID SnapshotID
	AsOf       time.Time
	ExpiresAt  time.Time
}

func (s FreezeState) Active(now time.Time) bool {
	return s.SnapshotID != "" && now.Before(s.ExpiresAt)
}

var (
	ErrFrozen                  = errors.New(loyaltyrpc.RefusalFrozen)
	ErrStaleFreezePin          = errors.New(loyaltyrpc.RefusalStalePin)
	ErrSnapshotGone            = errors.New(string(loyaltyrpc.RefusalSnapshotGone))
	ErrStandingsAccountRetired = errors.New("loyalty account retired")
)

func (r *Loyalty) Freeze(ctx context.Context, req FreezeRequest) (FreezeState, error) {
	if !req.valid() {
		return FreezeState{}, ErrInvalidInput
	}
	return withStandingsRow(ctx, r, req.UserID, func(ctx context.Context, tx *sql.Tx, prior FreezeState) (FreezeState, error) {
		if err := closePublicSnapshot(ctx, tx, snapshotKey{req.UserID, prior.SnapshotID}); err != nil {
			return FreezeState{}, err
		}
		now := time.Now().Truncate(time.Millisecond)
		state, err := copyStandings(ctx, tx, snapshotCopy{userID: req.UserID, asOf: now, expiresAt: now.Add(time.Duration(req.Minutes) * time.Minute)})
		if err != nil {
			return FreezeState{}, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE loyalty_freezes SET snapshot_id=?, as_of_ms=?, expires_at_ms=? WHERE user_id=?`,
			state.SnapshotID, state.AsOf.UnixMilli(), state.ExpiresAt.UnixMilli(), req.UserID)
		return state, err
	})
}

func (req FreezeRequest) valid() bool {
	return req.UserID != 0 && loyaltyrpc.ValidFreezeMinutes(req.Minutes)
}

func (r *Loyalty) ReleaseFreeze(ctx context.Context, userID uint64) (bool, error) {
	if userID == 0 {
		return false, ErrInvalidInput
	}
	return withStandingsRow(ctx, r, userID, func(ctx context.Context, tx *sql.Tx, state FreezeState) (bool, error) {
		if !state.Active(time.Now()) {
			return false, nil
		}
		if err := closePublicSnapshot(ctx, tx, snapshotKey{userID, state.SnapshotID}); err != nil {
			return false, err
		}
		_, err := tx.ExecContext(ctx, `UPDATE loyalty_freezes SET snapshot_id='', as_of_ms=0, expires_at_ms=0 WHERE user_id=?`, userID)
		return err == nil, err
	})
}

func closePublicSnapshot(ctx context.Context, tx *sql.Tx, key snapshotKey) error {
	now := time.Now().UnixMilli()
	_, err := tx.ExecContext(ctx, `UPDATE loyalty_snapshots SET expires_at_ms=? WHERE user_id=? AND snapshot_id=? AND expires_at_ms>?`,
		now, key.userID, key.id, now)
	return err
}

const freezeRow = `SELECT snapshot_id, as_of_ms, expires_at_ms FROM loyalty_freezes WHERE user_id=?`

func (r *Loyalty) FreezeStatus(ctx context.Context, userID uint64) (FreezeState, error) {
	return db.WithRead(ctx, func(ctx context.Context) (FreezeState, error) {
		state, err := scanStandingsState(r.sqldb.QueryRowContext(ctx, freezeRow, userID))
		if errors.Is(err, sql.ErrNoRows) {
			return FreezeState{}, nil
		}
		return state, err
	})
}

func scanStandingsState(row *sql.Row) (FreezeState, error) {
	var state FreezeState
	var asOf, expires int64
	err := row.Scan(&state.SnapshotID, &asOf, &expires)
	if err != nil || state.SnapshotID == "" {
		return FreezeState{}, err
	}
	state.AsOf, state.ExpiresAt = time.UnixMilli(asOf), time.UnixMilli(expires)
	return state, nil
}

type standingsMutation[T any] func(context.Context, *sql.Tx, FreezeState) (T, error)

func withStandingsRow[T any](ctx context.Context, r *Loyalty, userID uint64, fn standingsMutation[T]) (T, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (T, error) {
		var result T
		err := retryTx(ctx, func(ctx context.Context) error {
			var err error
			result, err = mutateStandingsRow(ctx, r, userID, fn)
			return err
		})
		return result, err
	})
}

func mutateStandingsRow[T any](ctx context.Context, r *Loyalty, userID uint64, fn standingsMutation[T]) (T, error) {
	var zero T
	tx, err := r.sqldb.BeginTx(ctx, db.ReadCommittedTx(r.dialect))
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	state, err := r.lockFreezeRow(ctx, tx, userID)
	if err != nil {
		return zero, err
	}
	result, err := fn(ctx, tx, state)
	if err != nil {
		return zero, err
	}
	return result, tx.Commit()
}

func (r *Loyalty) lockFreezeRow(ctx context.Context, tx *sql.Tx, userID uint64) (FreezeState, error) {
	if _, err := r.createFreezeRow(ctx, tx, userID); err != nil {
		return FreezeState{}, err
	}
	query := freezeRow
	if r.dialect != "sqlite3" {
		query += " FOR UPDATE"
	}
	return scanStandingsState(tx.QueryRowContext(ctx, query, userID))
}

func (r *Loyalty) createFreezeRow(ctx context.Context, tx *sql.Tx, userID uint64) (sql.Result, error) {
	stmt := "INSERT INTO loyalty_freezes (user_id) VALUES (?) ON DUPLICATE KEY UPDATE user_id=user_id"
	if r.dialect == "sqlite3" {
		stmt = "INSERT OR IGNORE INTO loyalty_freezes (user_id) VALUES (?)"
	}
	return tx.ExecContext(ctx, stmt, userID)
}
