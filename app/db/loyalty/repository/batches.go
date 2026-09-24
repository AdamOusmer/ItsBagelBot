// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"fmt"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/db"
)

// ApplyBumps commits one already-batched producer window. The receipt and every
// counter chunk share a transaction, so redelivery after an uncertain commit
// cannot add the same window twice. Returning an error asks pkg/bus to redeliver.
func (r *Loyalty) ApplyBumps(ctx context.Context, dto data.CounterBumpedDTO) error {
	if dto.BatchID == "" || len(dto.BatchID) > 255 {
		return fmt.Errorf("%w: counter batch ID", ErrInvalidInput)
	}
	bumps := make(map[bumpKey]*bumpSum)
	window := &Loyalty{bumpPend: bumps}
	for _, bump := range dto.Bumps {
		key, scope, ok := bumpTarget(dto.UserID, bump)
		if !ok {
			continue
		}
		if bump.Delta < -data.MaxCounter || bump.Delta > data.MaxCounter {
			return fmt.Errorf("%w: counter delta", ErrInvalidInput)
		}
		previous := int64(0)
		if sum := bumps[key]; sum != nil {
			previous = sum.delta
		}
		if (bump.Delta > 0 && previous > data.MaxCounter-bump.Delta) ||
			(bump.Delta < 0 && previous < -data.MaxCounter-bump.Delta) {
			return fmt.Errorf("%w: counter batch overflow", ErrInvalidInput)
		}
		window.foldBump(key, scope, bump)
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		tx, err := r.sqldb.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		result, err := tx.ExecContext(ctx, "INSERT IGNORE INTO counter_batches (id, created_at) VALUES (?, ?)", dto.BatchID, time.Now())
		if err != nil {
			return err
		}
		added, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if added == 0 {
			return nil
		}
		var writeErr error
		writer := &Loyalty{writeChunk: func(ctx context.Context, stmt chunkStmt) {
			if writeErr == nil {
				_, writeErr = tx.ExecContext(ctx, stmt.sql, stmt.args...)
			}
		}}
		writer.flushBumps(ctx, nil, bumps)
		if writeErr != nil {
			return writeErr
		}
		return tx.Commit()
	})
}
