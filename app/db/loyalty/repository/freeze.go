// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"maps"
	"strings"
	"time"

	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/codec"
	"ItsBagelBot/pkg/db"

	"github.com/nats-io/nuid"
)

type FreezeHolder string
type FreezeID string

type FreezeFlags struct {
	BlockTransfer bool
	CloseGames    bool
}

type FreezeRequest struct {
	UserID  uint64
	Holder  FreezeHolder
	Minutes int
	Flags   FreezeFlags
}

type FreezeRelease struct {
	UserID   uint64
	Holder   FreezeHolder
	FreezeID FreezeID
}

type FreezeState struct {
	FreezeID  FreezeID
	AsOf      time.Time
	ExpiresAt time.Time
	Holders   map[FreezeHolder]int64
	Flags     FreezeFlags
}

func (s FreezeState) Active(now time.Time) bool {
	return s.FreezeID != "" && now.Before(s.ExpiresAt)
}

func (s FreezeState) roundHeld(now time.Time) bool {
	for holder, end := range s.Holders {
		if holder.round() && end > now.UnixMilli() {
			return true
		}
	}
	return false
}

func (h FreezeHolder) round() bool {
	return strings.HasPrefix(string(h), loyaltyrpc.FreezeRoundPrefix)
}

var (
	ErrFrozen            = errors.New(loyaltyrpc.RefusalFrozen)
	ErrFreezeHeldByRound = errors.New(loyaltyrpc.RefusalHeldByRound)
	ErrStaleFreezePin    = errors.New(loyaltyrpc.RefusalStalePin)
	ErrFrozenTransfers   = errors.New(loyaltyrpc.RefusalFrozenTransfers)
	ErrFrozenGames       = errors.New(loyaltyrpc.RefusalFrozenGames)
)

type freezeChange struct {
	state   FreezeState
	changed bool
}

type freezeMutation func(context.Context, *sql.Tx, FreezeState) (freezeChange, error)

func (r *Loyalty) Freeze(ctx context.Context, req FreezeRequest) (FreezeState, bool, error) {
	if !req.valid() {
		return FreezeState{}, false, ErrInvalidInput
	}
	change, err := r.withFreezeRow(ctx, req.UserID, func(ctx context.Context, tx *sql.Tx, state FreezeState) (freezeChange, error) {
		now := time.Now().Truncate(time.Millisecond)
		if state.Active(now) {
			return state.join(now, req), nil
		}
		return startFreeze(ctx, tx, req)
	})
	return change.state, change.changed, err
}

func (req FreezeRequest) valid() bool {
	return req.UserID != 0 && req.Holder != "" && req.validDuration()
}

func (req FreezeRequest) validDuration() bool {
	return req.Minutes > 0 && req.Minutes <= int(loyaltyrpc.FreezeMaximum/time.Minute)
}

func (s FreezeState) join(now time.Time, req FreezeRequest) freezeChange {
	if s.Holders[req.Holder] > now.UnixMilli() {
		return freezeChange{state: s}
	}
	end := s.ExpiresAt.UnixMilli()
	if !req.Holder.round() {
		end = min(end, now.Add(time.Duration(req.Minutes)*time.Minute).UnixMilli())
	}
	maps.DeleteFunc(s.Holders, func(_ FreezeHolder, end int64) bool { return end <= now.UnixMilli() })
	s.Holders[req.Holder] = end
	return freezeChange{state: s, changed: true}
}

func startFreeze(ctx context.Context, tx *sql.Tx, req FreezeRequest) (freezeChange, error) {
	id := FreezeID(nuid.Next())
	asOf := time.Now().Truncate(time.Millisecond)
	if _, err := tx.ExecContext(ctx, freezeCopy, id, req.UserID); err != nil {
		return freezeChange{}, err
	}
	duration := time.Duration(req.Minutes) * time.Minute
	if req.Holder.round() {
		duration = loyaltyrpc.FreezeMaximum
	}
	end := asOf.Add(duration)
	state := FreezeState{FreezeID: id, AsOf: asOf, ExpiresAt: end, Flags: req.Flags, Holders: map[FreezeHolder]int64{req.Holder: end.UnixMilli()}}
	_, err := tx.ExecContext(ctx, `INSERT INTO loyalty_freeze_epochs (user_id, freeze_id, ends_at_ms, as_of_ms) VALUES (?,?,?,?)`,
		req.UserID, id, end.UnixMilli(), asOf.UnixMilli())
	return freezeChange{state: state, changed: true}, err
}

func (r *Loyalty) ReleaseFreeze(ctx context.Context, req FreezeRelease) (FreezeState, bool, error) {
	if req.UserID == 0 || !req.canCompare() {
		return FreezeState{}, false, ErrInvalidInput
	}
	change, err := r.withFreezeRow(ctx, req.UserID, func(_ context.Context, _ *sql.Tx, state FreezeState) (freezeChange, error) {
		return state.release(time.Now(), req)
	})
	return change.state, change.changed, err
}

func (req FreezeRelease) canCompare() bool {
	return req.Holder != "" && (req.FreezeID != "" || req.Holder == loyaltyrpc.FreezeModHolder)
}

func (s FreezeState) release(now time.Time, req FreezeRelease) (freezeChange, error) {
	if !s.Active(now) || s.mismatched(req.FreezeID) {
		return freezeChange{state: s}, nil
	}
	if req.Holder == loyaltyrpc.FreezeModHolder && s.roundHeld(now) {
		return freezeChange{}, ErrFreezeHeldByRound
	}
	if s.Holders[req.Holder] <= now.UnixMilli() {
		return freezeChange{state: s}, nil
	}
	delete(s.Holders, req.Holder)
	maps.DeleteFunc(s.Holders, func(_ FreezeHolder, end int64) bool { return end <= now.UnixMilli() })
	end := now.UnixMilli()
	for _, heldUntil := range s.Holders {
		end = max(end, heldUntil)
	}
	s.ExpiresAt = time.UnixMilli(end)
	return freezeChange{state: s, changed: true}, nil
}

func (s FreezeState) mismatched(id FreezeID) bool {
	return id != "" && id != s.FreezeID
}

const freezeRow = `SELECT freeze_id, as_of_ms, expires_at_ms, holders, block_transfer, close_games FROM loyalty_freezes WHERE user_id=?`

func (r *Loyalty) FreezeStatus(ctx context.Context, userID uint64) (FreezeState, error) {
	return db.WithRead(ctx, func(ctx context.Context) (FreezeState, error) {
		return scanFreeze(r.sqldb.QueryRowContext(ctx, freezeRow, userID))
	})
}

func scanFreeze(row *sql.Row) (FreezeState, error) {
	var state FreezeState
	var asOf, expires int64
	var holders []byte
	err := row.Scan(&state.FreezeID, &asOf, &expires, &holders, &state.Flags.BlockTransfer, &state.Flags.CloseGames)
	if errors.Is(err, sql.ErrNoRows) {
		return FreezeState{}, nil
	}
	if err != nil {
		return FreezeState{}, err
	}
	if state.FreezeID == "" {
		return FreezeState{}, nil
	}
	state.AsOf, state.ExpiresAt = time.UnixMilli(asOf), time.UnixMilli(expires)
	return state, codec.Unmarshal(holders, &state.Holders)
}

func (r *Loyalty) withFreezeRow(ctx context.Context, userID uint64, fn freezeMutation) (freezeChange, error) {
	return db.WithQuery(ctx, func(ctx context.Context) (freezeChange, error) {
		var change freezeChange
		err := retryTx(ctx, func(ctx context.Context) error {
			var err error
			change, err = r.mutateFreezeRow(ctx, userID, fn)
			return err
		})
		return change, err
	})
}

func (r *Loyalty) mutateFreezeRow(ctx context.Context, userID uint64, fn freezeMutation) (freezeChange, error) {
	tx, err := r.sqldb.BeginTx(ctx, db.ReadCommittedTx(r.dialect))
	if err != nil {
		return freezeChange{}, err
	}
	defer tx.Rollback()
	state, err := r.lockFreezeRow(ctx, tx, userID)
	if err != nil {
		return freezeChange{}, err
	}
	change, err := fn(ctx, tx, state)
	if err != nil {
		return freezeChange{}, err
	}
	if err := change.save(ctx, tx, userID); err != nil {
		return freezeChange{}, err
	}
	return change, tx.Commit()
}

func (r *Loyalty) lockFreezeRow(ctx context.Context, tx *sql.Tx, userID uint64) (FreezeState, error) {
	if _, err := r.createFreezeRow(ctx, tx, userID); err != nil {
		return FreezeState{}, err
	}
	query := freezeRow
	if r.dialect != "sqlite3" {
		query += " FOR UPDATE"
	}
	return scanFreeze(tx.QueryRowContext(ctx, query, userID))
}

func (r *Loyalty) createFreezeRow(ctx context.Context, tx *sql.Tx, userID uint64) (sql.Result, error) {
	stmt := "INSERT INTO loyalty_freezes (user_id, holders) VALUES (?, '{}') ON DUPLICATE KEY UPDATE user_id=user_id"
	if r.dialect == "sqlite3" {
		stmt = "INSERT OR IGNORE INTO loyalty_freezes (user_id, holders) VALUES (?, '{}')"
	}
	return tx.ExecContext(ctx, stmt, userID)
}

func (c freezeChange) save(ctx context.Context, tx *sql.Tx, userID uint64) error {
	if !c.changed {
		return nil
	}
	s := c.state
	holders, err := codec.Marshal(s.Holders)
	if err != nil {
		return err
	}
	end := s.ExpiresAt.UnixMilli()
	if _, err := tx.ExecContext(ctx, `UPDATE loyalty_freezes SET freeze_id=?, as_of_ms=?, expires_at_ms=?, holders=?, block_transfer=?, close_games=? WHERE user_id=?`,
		s.FreezeID, s.AsOf.UnixMilli(), end, string(holders), s.Flags.BlockTransfer, s.Flags.CloseGames, userID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE loyalty_freeze_epochs SET ends_at_ms=? WHERE user_id=? AND freeze_id=?`, end, userID, s.FreezeID)
	return err
}
