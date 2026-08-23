// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestRearmAfterFailure(t *testing.T) {
	// The first two failures may be blips: retry inside a minute so a
	// transient chatters/live error costs viewers a minute, not a window.
	assert.Equal(t, watchTickQuickRetry, rearmAfterFailure(1))
	assert.Equal(t, watchTickQuickRetry, rearmAfterFailure(watchTickQuickRetries))
	// Past the cap, fall back to the normal cadence instead of spending Helix
	// calls on a hard-down dependency every minute.
	assert.Equal(t, watchTickInterval, rearmAfterFailure(watchTickQuickRetries+1))
	assert.Equal(t, watchTickInterval, rearmAfterFailure(50))
}

func TestWatchTickFailureStreak(t *testing.T) {
	c := &ValkeyLoyaltyClock{log: zap.NewNop(), failures: map[uint64]int{}, fires: map[uint64]int{}}

	assert.Equal(t, watchTickQuickRetry, c.afterFailure(7, errors.New("boom")))
	assert.Equal(t, watchTickQuickRetry, c.afterFailure(7, errors.New("boom")))
	assert.Equal(t, watchTickInterval, c.afterFailure(7, errors.New("boom")))

	// A good tick clears the streak: escalation measures a continuous problem.
	c.resetFailures(7)
	assert.Equal(t, watchTickQuickRetry, c.afterFailure(7, errors.New("boom")))

	// Channels ledger independently.
	assert.Len(t, c.failures, 1)
}

func TestWatchTickFireCountAndForget(t *testing.T) {
	c := &ValkeyLoyaltyClock{log: zap.NewNop(), failures: map[uint64]int{}, fires: map[uint64]int{}}

	assert.Equal(t, 1, c.noteFire(7))
	assert.Equal(t, 2, c.noteFire(7))
	assert.Equal(t, 1, c.noteFire(9), "channels count independently")

	// Disarm's ledger cleanup drops both maps for the channel; the next
	// session starts from zero streaks and a fresh reconfirm cadence.
	assert.Equal(t, watchTickQuickRetry, c.afterFailure(9, errors.New("boom")))
	c.forget(9)
	assert.NotContains(t, c.fires, 9)
	assert.NotContains(t, c.failures, 9)
}
