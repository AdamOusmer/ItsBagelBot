// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ItsBagelBot/app/db/loyalty/ent"
	"ItsBagelBot/pkg/db"

	"github.com/nats-io/nuid"
)

type SnapshotOwner string

type SnapshotRequest struct {
	UserID    uint64
	Owner     SnapshotOwner
	ExpiresAt time.Time
}

type snapshotKey struct {
	userID uint64
	id     SnapshotID
}

type snapshotCopy struct {
	userID          uint64
	owner           SnapshotOwner
	asOf, expiresAt time.Time
}

func (r *Loyalty) CreateSnapshot(ctx context.Context, req SnapshotRequest) (FreezeState, error) {
	if req.UserID == 0 || !req.validOwner() {
		return FreezeState{}, ErrInvalidInput
	}
	return withStandingsRow(ctx, r, req.UserID, func(ctx context.Context, tx *sql.Tx, _ FreezeState) (FreezeState, error) {
		return req.create(ctx, tx)
	})
}

func (req SnapshotRequest) validOwner() bool {
	return len(req.Owner) > 0 && len(req.Owner) <= 255
}

func (req SnapshotRequest) create(ctx context.Context, tx *sql.Tx) (FreezeState, error) {
	row := tx.QueryRowContext(ctx, `SELECT snapshot_id,as_of_ms,expires_at_ms FROM loyalty_snapshots WHERE user_id=? AND owner=?`, req.UserID, []byte(req.Owner))
	state, err := scanStandingsState(row)
	now := time.Now().Truncate(time.Millisecond)
	switch {
	case err == nil && !state.Active(now):
		return FreezeState{}, ErrSnapshotGone
	case !errors.Is(err, sql.ErrNoRows):
		return state, err
	}
	end := req.ExpiresAt.Truncate(time.Millisecond)
	if !end.After(now) {
		return FreezeState{}, ErrInvalidInput
	}
	return copyStandings(ctx, tx, snapshotCopy{userID: req.UserID, owner: req.Owner, asOf: now, expiresAt: end})
}

const freezeCopy = `INSERT INTO loyalty_freeze_balances (user_id, snapshot_id, viewer_id, points, watch_seconds, login, viewer_name)
	SELECT user_id, ?, viewer_id, points, watch_seconds, COALESCE(viewer_login, ''), COALESCE(viewer_name, '')
	FROM balances WHERE user_id=?`

func copyStandings(ctx context.Context, tx *sql.Tx, copy snapshotCopy) (FreezeState, error) {
	account, err := readAccountFence(tx.QueryRowContext(ctx, accountFenceQuery, copy.userID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return FreezeState{}, err
	}
	if account.deleted {
		return FreezeState{}, ErrStandingsAccountRetired
	}
	state := FreezeState{SnapshotID: SnapshotID(nuid.Next()), AsOf: copy.asOf, ExpiresAt: copy.expiresAt}
	if err := copy.insert(ctx, tx, state.SnapshotID); err != nil {
		return FreezeState{}, err
	}
	_, err = tx.ExecContext(ctx, freezeCopy, state.SnapshotID, copy.userID)
	return state, err
}

func (c snapshotCopy) insert(ctx context.Context, tx *sql.Tx, id SnapshotID) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO loyalty_snapshots (user_id,snapshot_id,owner,as_of_ms,expires_at_ms)
		VALUES (?,?,?,?,?)`, c.userID, id, c.ownerValue(), c.asOf.UnixMilli(), c.expiresAt.UnixMilli())
	return err
}

func (c snapshotCopy) ownerValue() any {
	if c.owner == "" {
		return nil
	}
	return []byte(c.owner)
}

func (r *Loyalty) DropSnapshot(ctx context.Context, userID uint64, id SnapshotID) (bool, error) {
	if userID == 0 || id == "" {
		return false, ErrInvalidInput
	}
	return db.WithQuery(ctx, func(ctx context.Context) (bool, error) {
		return dropPrivateSnapshot(ctx, r.sqldb, snapshotKey{userID, id})
	})
}

func dropPrivateSnapshot(ctx context.Context, sqlDB *sql.DB, key snapshotKey) (bool, error) {
	now := time.Now().UnixMilli()
	result, err := sqlDB.ExecContext(ctx, `UPDATE loyalty_snapshots SET expires_at_ms=?
		WHERE user_id=? AND snapshot_id=? AND owner IS NOT NULL AND expires_at_ms>?`, now, key.userID, key.id, now)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n != 0, err
}

const privateSnapshotJoin = ` FROM loyalty_snapshots s LEFT JOIN loyalty_freeze_balances b
	ON b.user_id=s.user_id AND b.snapshot_id=s.snapshot_id`
const privateSnapshotScope = ` WHERE s.user_id=? AND s.snapshot_id=? AND s.owner IS NOT NULL AND s.expires_at_ms>?`

func (r *Loyalty) SnapshotBalance(ctx context.Context, userID, viewerID uint64, id SnapshotID) (*ent.Balance, bool, error) {
	row, err := db.WithRead(ctx, func(ctx context.Context) (*ent.Balance, error) {
		query := `SELECT s.user_id,COALESCE(b.viewer_id,0),COALESCE(b.points,0),COALESCE(b.watch_seconds,0),
			COALESCE(b.login,''),COALESCE(b.viewer_name,'')` + privateSnapshotJoin + ` AND b.viewer_id=?` + privateSnapshotScope
		return scanFrozenBalance(r.sqldb.QueryRowContext(ctx, query, viewerID, userID, id, time.Now().UnixMilli()))
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, ErrSnapshotGone
	}
	if err != nil || row.ViewerID == 0 {
		return nil, false, err
	}
	return row, true, nil
}
