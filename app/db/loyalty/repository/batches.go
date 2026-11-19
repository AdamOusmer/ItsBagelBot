// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"errors"
	"fmt"

	"ItsBagelBot/internal/domain/event/data"
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
