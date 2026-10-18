// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"ItsBagelBot/internal/domain/event/data"
)

const (
	readTrialPromotion = "SELECT 1 FROM counters WHERE user_id = ? AND name = ?"
	lockTrialPromotion = readTrialPromotion + " FOR SHARE"
)

var errTrialPromotedMidBatch = errors.New("trial promoted while the counter batch was written")

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

func trialPromoted(ctx context.Context, tx *sql.Tx, query string, userID uint64) (bool, error) {
	var one int
	err := tx.QueryRowContext(ctx, query, userID, data.CounterTrialPromoted).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (r *Loyalty) counterBatchRetired(ctx context.Context, tx *sql.Tx, id uint64) (bool, error) {
	if !r.watchReady.Load() {
		return false, nil
	}
	if id == 0 {
		return false, nil
	}
	_, retired, err := r.fence(ctx, tx, id, 0)
	return retired, err
}
