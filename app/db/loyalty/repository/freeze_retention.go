// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"
)

const freezePruneGrace = 2 * time.Minute

func (r *Loyalty) PruneFreezeSnapshots(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, watchHistoryPruneBudget)
	defer cancel()
	snapshots, err := r.expiredSnapshots(ctx)
	if err != nil {
		return err
	}
	for i := 0; i < len(snapshots) && ctx.Err() == nil; i++ {
		if err := r.pruneSnapshot(ctx, snapshots[i]); err != nil {
			return err
		}
	}
	return nil
}

func (r *Loyalty) expiredSnapshots(ctx context.Context) ([]snapshotKey, error) {
	now := time.Now()
	rows, err := r.sqldb.QueryContext(ctx, `SELECT user_id, snapshot_id FROM loyalty_snapshots WHERE expires_at_ms < ? AND (owner IS NOT NULL OR expires_at_ms < ?)
		ORDER BY expires_at_ms LIMIT 16`,
		now.UnixMilli(), now.Add(-freezePruneGrace).UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var snapshots []snapshotKey
	for rows.Next() {
		var key snapshotKey
		if err := rows.Scan(&key.userID, &key.id); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, key)
	}
	return snapshots, rows.Err()
}

func (r *Loyalty) pruneSnapshot(ctx context.Context, key snapshotKey) error {
	if _, err := r.sqldb.ExecContext(ctx, `UPDATE loyalty_snapshots SET pruning=1 WHERE user_id=? AND snapshot_id=?`, key.userID, key.id); err != nil {
		return err
	}
	prune := receiptPrune{table: frozenBalances, condition: "user_id=? AND snapshot_id=? ORDER BY viewer_id", args: []any{key.userID, key.id}}
	for ctx.Err() == nil {
		more, err := r.pruneReceiptChunk(ctx, prune)
		if err != nil {
			return err
		}
		if !more {
			return r.deleteSnapshot(ctx, key)
		}
	}
	return nil
}

func (r *Loyalty) deleteSnapshot(ctx context.Context, key snapshotKey) error {
	_, err := r.sqldb.ExecContext(ctx, `DELETE FROM loyalty_snapshots WHERE user_id=? AND snapshot_id=?`, key.userID, key.id)
	return err
}
