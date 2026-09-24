// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
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
	bumps, err := collectBatchBumps(dto)
	if err != nil {
		return err
	}
	return db.WithExec(ctx, func(ctx context.Context) error {
		return r.commitBumpBatch(ctx, dto.BatchID, bumps)
	})
}

func collectBatchBumps(dto data.CounterBumpedDTO) (map[bumpKey]*bumpSum, error) {
	window := &Loyalty{bumpPend: make(map[bumpKey]*bumpSum)}
	for _, bump := range dto.Bumps {
		if err := window.collectBatchBump(dto.UserID, bump); err != nil {
			return nil, err
		}
	}
	return window.bumpPend, nil
}

func (r *Loyalty) collectBatchBump(userID uint64, bump data.CounterBumpEntry) error {
	key, scope, ok := bumpTarget(userID, bump)
	if !ok {
		return nil
	}
	previous := int64(0)
	if sum := r.bumpPend[key]; sum != nil {
		previous = sum.delta
	}
	if err := validateBatchDelta(previous, bump.Delta); err != nil {
		return err
	}
	r.foldBump(key, scope, bump)
	return nil
}

func validateBatchDelta(previous, delta int64) error {
	if delta < -data.MaxCounter || delta > data.MaxCounter {
		return fmt.Errorf("%w: counter delta", ErrInvalidInput)
	}
	if delta > 0 && previous > data.MaxCounter-delta {
		return fmt.Errorf("%w: counter batch overflow", ErrInvalidInput)
	}
	if delta < 0 && previous < -data.MaxCounter-delta {
		return fmt.Errorf("%w: counter batch overflow", ErrInvalidInput)
	}
	return nil
}

func (r *Loyalty) commitBumpBatch(ctx context.Context, batchID string, bumps map[bumpKey]*bumpSum) error {
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	added, err := insertBatchReceipt(ctx, tx, batchID)
	if err != nil || !added {
		return err
	}
	if err := writeBatchBumps(ctx, tx, bumps); err != nil {
		return err
	}
	return tx.Commit()
}

func insertBatchReceipt(ctx context.Context, tx *sql.Tx, batchID string) (bool, error) {
	result, err := tx.ExecContext(ctx, "INSERT IGNORE INTO counter_batches (id, created_at) VALUES (?, ?)", batchID, time.Now())
	if err != nil {
		return false, err
	}
	added, err := result.RowsAffected()
	return added != 0, err
}

func writeBatchBumps(ctx context.Context, tx *sql.Tx, bumps map[bumpKey]*bumpSum) error {
	var writeErr error
	writer := &Loyalty{writeChunk: func(ctx context.Context, stmt chunkStmt) {
		if writeErr == nil {
			_, writeErr = tx.ExecContext(ctx, stmt.sql, stmt.args...)
		}
	}}
	writer.flushBumps(ctx, nil, bumps)
	return writeErr
}
