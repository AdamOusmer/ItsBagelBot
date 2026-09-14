// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package ratelimit

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/pkg/kvstate/kvtest"
	"github.com/stretchr/testify/require"
)

func TestJetStreamReplicasShareBudgetAcrossRestarts(t *testing.T) {
	kv := kvtest.New()
	req := NewSpec(20, 20.0/30).ForKey("chat")
	var admitted atomic.Int64
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			manager := NewJetStreamManager(kv)
			for range 20 {
				ok, _ := manager.Allow(context.Background(), req)
				if ok {
					admitted.Add(1)
				}
			}
		})
	}
	wg.Wait()
	require.Equal(t, int64(20), admitted.Load())
	ok, err := NewJetStreamManager(kv).Allow(context.Background(), req)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestJetStreamRateFailsClosedAndRecovers(t *testing.T) {
	kv := kvtest.New()
	manager := NewJetStreamManager(kv)
	req := NewSpec(1, 1).ForKey("chat")
	kv.Err = errors.New("no quorum")
	ok, err := manager.Allow(context.Background(), req)
	require.Error(t, err)
	require.False(t, ok)
	kv.Err = nil
	ok, err = manager.Allow(context.Background(), req)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestDurableBudgetDoesNotReplayClockRollback(t *testing.T) {
	state := durableBudget{}
	req := NewSpec(1, 1).ForKey("test")
	now := time.Unix(100, 0)
	require.Zero(t, evaluateBudget(state, []Request{req}, now))
	require.Equal(t, uint8(1), evaluateBudget(state, []Request{req}, now.Add(-time.Minute)))
	require.Equal(t, uint8(1), evaluateBudget(state, []Request{req}, now))
}

func TestDurableBudgetDoesNotRefillFromZeroTimestamp(t *testing.T) {
	state := durableBudget{}
	req := NewSpec(2, 1).ForKey("test")
	require.Zero(t, evaluateBudget(state, []Request{req}, time.Time{}))
	now := time.Unix(100, 0)
	require.Zero(t, evaluateBudget(state, []Request{req}, now))
	require.Equal(t, uint8(1), evaluateBudget(state, []Request{req}, now))
	require.Zero(t, evaluateBudget(state, []Request{req}, now.Add(time.Second)))
}

func TestDurableOrderedDenialPreservesOtherBudget(t *testing.T) {
	state := durableBudget{}
	first := NewSpec(2, 1).ForKey("standard")
	second := NewSpec(1, 1).ForKey("shared")
	now := time.Unix(100, 0)
	require.Zero(t, evaluateBudget(state, []Request{second}, now))
	require.Equal(t, uint8(2), evaluateBudget(state, []Request{first, second}, now))
	require.Equal(t, 2.0, state[requestID(first)].Tokens)
}

func TestDurableBudgetClampsModeratorDowngrade(t *testing.T) {
	state := durableBudget{}
	mod := NewSpec(100, 100.0/30).ForKey("chat")
	ordinary := NewSpec(20, 20.0/30).ForKey("chat")
	now := time.Unix(100, 0)
	require.Zero(t, evaluateBudget(state, []Request{mod}, now))
	require.Zero(t, evaluateBudget(state, []Request{ordinary}, now))
	require.Equal(t, 19.0, state[requestID(ordinary)].Tokens)
}
