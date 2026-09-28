// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLiveAndWatchTimeShareFallbackVersion(t *testing.T) {
	tick := &versionedWatchTicker{completed: make(chan bool, 1)}
	fx := newLiveFixture()
	c := liveCtx("stream.online", "")
	require.NoError(t, fx.m.Events["stream.online"](context.Background(), c, func(*module.Output) {}))
	time.Sleep(2 * time.Millisecond)
	m := Loyalty(engine.Deps{LoyaltyTick: tick, Log: zap.NewNop()})
	require.NoError(t, m.Events["stream.online"](context.Background(), c, func(*module.Output) {}))
	select {
	case <-tick.completed:
	case <-time.After(time.Second):
		t.Fatal("lifecycle task did not finish")
	}
	waitForLog(t, fx.log, 1)
	fx.live.mu.Lock()
	defer fx.live.mu.Unlock()
	require.Len(t, fx.live.setCalls, 1)
	assert.Equal(t, fx.live.setCalls[0], tick.version)
}
