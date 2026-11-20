// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"ItsBagelBot/pkg/db"
)

func (r *Loyalty) ExtendSnapshot(ctx context.Context, req SnapshotExtension) (bool, error) {
	now := time.Now().UnixMilli()
	end := req.ExpiresAt.UnixMilli()
	if req.UserID == 0 || req.ID == "" || end <= now {
		return false, ErrInvalidInput
	}
	return db.WithQuery(ctx, func(ctx context.Context) (bool, error) {
		return r.extendSnapshot(ctx, req, now)
	})
}

func (r *Loyalty) extendSnapshot(ctx context.Context, req SnapshotExtension, now int64) (bool, error) {
	end := req.ExpiresAt.UnixMilli()
	result, err := r.sqldb.ExecContext(ctx, `UPDATE loyalty_snapshots SET expires_at_ms=?
		WHERE user_id=? AND snapshot_id=? AND owner IS NOT NULL AND expires_at_ms>? AND expires_at_ms<?`, end, req.UserID, req.ID, now, end)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 0 {
		return n != 0, err
	}
	var found int
	err = r.sqldb.QueryRowContext(ctx, `SELECT 1 FROM loyalty_snapshots
		WHERE user_id=? AND snapshot_id=? AND owner IS NOT NULL AND expires_at_ms>=?`, req.UserID, req.ID, end).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
