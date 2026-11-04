// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"sync/atomic"
	"testing"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type refusingLimiter struct {
	asked  atomic.Int64
	window atomic.Bool
}

func (l *refusingLimiter) AllowRun(context.Context, uint64) (bool, error) {
	l.asked.Add(1)
	return false, nil
}

func (l *refusingLimiter) FirstRefusal(uint64) bool { return !l.window.Swap(true) }

func (l *refusingLimiter) AllowLights(context.Context, uint64) (bool, error) { return false, nil }

type recordedLog struct {
	countingRunLog
	last atomic.Pointer[automationrpc.RunRecord]
}

func (l *recordedLog) Record(ctx context.Context, bid uint64, rec automationrpc.RunRecord) {
	l.countingRunLog.Record(ctx, bid, rec)
	l.last.Store(&rec)
}

func refusedRig(t testing.TB) (flowRig, *refusingLimiter) {
	rig := flowReplyRig(t)
	limiter := &refusingLimiter{}
	rig.p.automations.Limiter = limiter
	return rig, limiter
}

func TestRefusedTriggerRecordsOncePerWindow(t *testing.T) {
	rig, limiter := refusedRig(t)
	for range 200 {
		_ = rig.p.Process(rig.msg)
	}
	assert.EqualValues(t, 200, limiter.asked.Load())
	assert.EqualValues(t, 1, rig.runs.n.Load())

	limiter.window.Store(false)
	_ = rig.p.Process(rig.msg)
	assert.EqualValues(t, 2, rig.runs.n.Load())
}

func TestRefusedTriggerRecordsALimitedRun(t *testing.T) {
	rig, _ := refusedRig(t)
	log := &recordedLog{}
	rig.p.automations.Runs = log
	_ = rig.p.Process(rig.msg)

	rec := log.last.Load()
	require.NotNil(t, rec)
	assert.NotEmpty(t, rec.RunID)
	assert.Equal(t, automationrpc.OutcomeLimited, rec.Outcome)
	assert.Equal(t, "channel_rate", rec.Reason)
	assert.Equal(t, "command", rec.Source)
}

func TestRefusedTriggerAllocCeiling(t *testing.T) {
	rig, limiter := refusedRig(t)
	limiter.window.Store(true)
	avg := testing.AllocsPerRun(500, func() { _ = rig.p.Process(rig.msg) })
	assert.Zero(t, rig.runs.n.Load())
	assert.LessOrEqual(t, avg, allocFlowRefusedCeiling)
}

func TestValkeyRunLimiterReportsFirstRefusalPerWindow(t *testing.T) {
	ctx := context.Background()
	limiter := newValkeyRunLimiter(newHotPathTestClient(t))
	bid := uniqueBroadcaster()
	assert.False(t, limiter.FirstRefusal(bid))
	for range automation.MaxRunsPerMinute {
		ok, err := limiter.AllowRun(ctx, bid)
		require.NoError(t, err)
		require.True(t, ok)
	}
	ok, err := limiter.AllowRun(ctx, bid)
	require.NoError(t, err)
	require.False(t, ok)

	assert.True(t, limiter.FirstRefusal(bid))
	assert.False(t, limiter.FirstRefusal(bid))
}
