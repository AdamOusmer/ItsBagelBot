// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	assert.Equal(t, watchTickQuickRetry, c.settleFailure(7, errors.New("boom")))
	assert.Equal(t, watchTickQuickRetry, c.settleFailure(7, errors.New("boom")))
	assert.Equal(t, watchTickInterval, c.settleFailure(7, errors.New("boom")))
	assert.Len(t, c.failures, 1)
}

func TestWatchTickSettleSuccess(t *testing.T) {
	pub := &rawPublisher{payloads: map[string][][]byte{}}
	c := &ValkeyLoyaltyClock{
		log:                   zap.NewNop(),
		failures:              map[uint64]int{},
		fires:                 map[uint64]int{},
		pub:                   pub,
		outgressSystemSubject: "test.system",
	}
	ctx := context.Background()

	// A success clears a prior failure streak and re-arms at the interval.
	c.settleFailure(7, errors.New("boom"))
	assert.Equal(t, watchTickInterval, c.settleSuccess(ctx, 7))
	assert.Empty(t, c.failures, "a good tick clears the streak")

	// The next ten successes stay quiet; the twelfth fire since the streak
	// reset fires exactly one live re-check (watchTickReconfirmEvery),
	// addressed to the channel.
	for i := 0; i < watchTickReconfirmEvery-2; i++ {
		assert.Equal(t, watchTickInterval, c.settleSuccess(ctx, 7))
	}
	require.Empty(t, pub.payloads, "no re-check before the cadence is due")
	assert.Equal(t, watchTickInterval, c.settleSuccess(ctx, 7))

	frames := pub.payloads["test.system"]
	require.Len(t, frames, 1)
	var msg outgress.Message
	require.NoError(t, codec.Unmarshal(frames[0], &msg))
	assert.Equal(t, outgress.TypeStreamStatus, msg.Type)
	assert.Equal(t, "7", msg.BroadcasterID)

	// Channels ledger independently: channel 9's own twelfth fire is what
	// triggers its confirm.
	for i := 0; i < watchTickReconfirmEvery; i++ {
		c.settleSuccess(ctx, 9)
	}
	assert.Len(t, pub.payloads["test.system"], 2)
}
