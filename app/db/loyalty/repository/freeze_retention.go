// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"
)

const freezePruneGrace = 2 * time.Minute

type freezeEpochKey struct {
	userID uint64
	id     FreezeID
}

func (r *Loyalty) PruneFreezeSnapshots(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, watchHistoryPruneBudget)
	defer cancel()
	epochs, err := r.endedFreezeEpochs(ctx)
	if err != nil {
		return err
	}
	for i := 0; i < len(epochs) && ctx.Err() == nil; i++ {
		if err := r.pruneFreezeEpoch(ctx, epochs[i]); err != nil {
			return err
		}
	}
	return nil
}

func (r *Loyalty) endedFreezeEpochs(ctx context.Context) ([]freezeEpochKey, error) {
	rows, err := r.sqldb.QueryContext(ctx, `SELECT user_id, freeze_id FROM loyalty_freeze_epochs WHERE ends_at_ms<? ORDER BY ends_at_ms LIMIT 16`,
		time.Now().Add(-freezePruneGrace).UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var epochs []freezeEpochKey
	for rows.Next() {
		var epoch freezeEpochKey
		if err := rows.Scan(&epoch.userID, &epoch.id); err != nil {
			return nil, err
		}
		epochs = append(epochs, epoch)
	}
	return epochs, rows.Err()
}

func (r *Loyalty) pruneFreezeEpoch(ctx context.Context, epoch freezeEpochKey) error {
	if _, err := r.sqldb.ExecContext(ctx, `UPDATE loyalty_freeze_epochs SET pruning=1 WHERE user_id=? AND freeze_id=?`, epoch.userID, epoch.id); err != nil {
		return err
	}
	prune := receiptPrune{table: "loyalty_freeze_balances", condition: "user_id=? AND freeze_id=? ORDER BY viewer_id", args: []any{epoch.userID, epoch.id}}
	for ctx.Err() == nil {
		more, err := r.pruneReceiptChunk(ctx, prune)
		if err != nil {
			return err
		}
		if !more {
			return r.dropFreezeEpoch(ctx, epoch)
		}
	}
	return nil
}

func (r *Loyalty) dropFreezeEpoch(ctx context.Context, epoch freezeEpochKey) error {
	_, err := r.sqldb.ExecContext(ctx, `DELETE FROM loyalty_freeze_epochs WHERE user_id=? AND freeze_id=?`, epoch.userID, epoch.id)
	return err
}
