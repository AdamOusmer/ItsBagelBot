// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
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
	batch := counterBatch{dto: dto, bumps: bumps, trial: batchTrialBumps(dto.UserID, bumps)}
	return db.WithExec(ctx, func(ctx context.Context) error {
		return retryTx(ctx, func(ctx context.Context) error {
			return r.commitBumpBatch(ctx, batch)
		}, errTrialPromotedMidBatch)
	})
}

const (
	readTrialPromotion = "SELECT 1 FROM counters WHERE user_id = ? AND name = ?"
	lockTrialPromotion = readTrialPromotion + " FOR SHARE"
)

var errTrialPromotedMidBatch = errors.New("trial promoted while the counter batch was written")

type counterBatch struct {
	dto   data.CounterBumpedDTO
	bumps map[bumpKey]*bumpSum
	trial trialTotals
}

func batchTrialBumps(userID uint64, bumps map[bumpKey]*bumpSum) trialTotals {
	return trialTotals{
		decoded:  channelDelta(bumps, userID, data.CounterTrialDecoded),
		answered: channelDelta(bumps, userID, data.CounterTrialAnswered),
	}
}

func channelDelta(bumps map[bumpKey]*bumpSum, userID uint64, name string) int64 {
	sum := bumps[bumpKey{userID: userID, name: name}]
	if sum == nil || sum.scope != data.CounterScopeChannel {
		return 0
	}
	return max(sum.delta, 0)
}

func (b counterBatch) redirected() (map[bumpKey]*bumpSum, error) {
	dto := b.dto
	dto.Bumps = slices.Clone(dto.Bumps)
	for _, carry := range b.trial.carried() {
		dto.Bumps = append(dto.Bumps, data.CounterBumpEntry{Name: carry.name, Scope: data.CounterScopeChannel, Delta: carry.delta})
	}
	return collectBatchBumps(dto)
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

func (r *Loyalty) commitBumpBatch(ctx context.Context, batch counterBatch) error {
	tx, err := r.sqldb.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	added, err := insertBatchReceipt(ctx, tx, batch.dto.BatchID)
	if err != nil || !added {
		return err
	}
	if err := writeCounterBatch(ctx, tx, batch); err != nil {
		return err
	}
	return tx.Commit()
}

func writeCounterBatch(ctx context.Context, tx *sql.Tx, batch counterBatch) error {
	if batch.trial == (trialTotals{}) {
		return writeBatchBumps(ctx, tx, batch.bumps)
	}
	promoted, err := trialPromoted(ctx, tx, readTrialPromotion, batch.dto.UserID)
	if err != nil {
		return err
	}
	if promoted {
		return writeRedirectedBumps(ctx, tx, batch)
	}
	return writeUnpromotedTrialBumps(ctx, tx, batch)
}

// The promotion re-check must follow the trial row writes; checking earlier lets a promotion commit in between and lose the bump.
func writeUnpromotedTrialBumps(ctx context.Context, tx *sql.Tx, batch counterBatch) error {
	if err := writeBatchBumps(ctx, tx, batch.bumps); err != nil {
		return err
	}
	promoted, err := trialPromoted(ctx, tx, lockTrialPromotion, batch.dto.UserID)
	if err != nil {
		return err
	}
	if promoted {
		return errTrialPromotedMidBatch
	}
	return nil
}

func writeRedirectedBumps(ctx context.Context, tx *sql.Tx, batch counterBatch) error {
	bumps, err := batch.redirected()
	if err != nil {
		return err
	}
	return writeBatchBumps(ctx, tx, bumps)
}

func trialPromoted(ctx context.Context, tx *sql.Tx, query string, userID uint64) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx, query, userID, data.CounterTrialPromoted).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
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
