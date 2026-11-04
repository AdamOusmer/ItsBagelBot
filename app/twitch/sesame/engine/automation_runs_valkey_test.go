// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func uniqueBroadcaster() uint64 { return uint64(time.Now().UnixNano()) }

func runRec(runID, automationID string) automationrpc.RunRecord {
	return automationrpc.RunRecord{RunID: runID, AutomationID: automationID, Name: automationID, Trigger: "manual", Source: "api", Outcome: automationrpc.OutcomeOK}
}

func TestValkeyRunLogReturnsNewestFirstWithFilterAndLimit(t *testing.T) {
	ctx := context.Background()
	runs := NewValkeyRunLog(newHotPathTestClient(t), zap.NewNop())
	bid := uniqueBroadcaster()
	for i, id := range []string{"a", "b", "a", "b", "a"} {
		runs.Record(ctx, bid, runRec("r"+strconv.Itoa(i), id))
	}

	all, err := runs.Recent(ctx, bid, "", 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"r4", "r3", "r2", "r1", "r0"}, runIDs(all))

	onlyA, err := runs.Recent(ctx, bid, "a", 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"r4", "r2"}, runIDs(onlyA))
}

func TestValkeyRunLogKeepsOnlyRunsKept(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	runs := NewValkeyRunLog(client, zap.NewNop())
	bid := uniqueBroadcaster()
	for i := 0; i < automation.RunsKept+10; i++ {
		runs.Record(ctx, bid, runRec("r"+strconv.Itoa(i), "a"))
	}

	got, err := runs.Recent(ctx, bid, "", 1000)
	require.NoError(t, err)
	require.Len(t, got, automation.RunsKept)
	assert.Equal(t, "r59", got[0].RunID)
	requirePositiveTTL(t, ctx, client, runLogKey(bid))
}

func TestValkeyRunLimiterAllowsUpToChannelCap(t *testing.T) {
	ctx := context.Background()
	limiter := newValkeyRunLimiter(newHotPathTestClient(t))
	limiter.now = func() time.Time { return time.Unix(1_800_000_000, 0) }
	bid := uniqueBroadcaster()

	for i := 0; i < automation.MaxRunsPerMinute; i++ {
		ok, err := limiter.AllowRun(ctx, bid)
		require.NoError(t, err)
		require.True(t, ok, "run %d", i)
	}
	ok, err := limiter.AllowRun(ctx, bid)
	require.NoError(t, err)
	assert.False(t, ok)

	limiter.now = func() time.Time { return time.Unix(1_800_000_060, 0) }
	ok, err = limiter.AllowRun(ctx, bid)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestValkeyRunLimiterCapsLightsPerMinute(t *testing.T) {
	ctx := context.Background()
	limiter := newValkeyRunLimiter(newHotPathTestClient(t))
	at := time.Unix(6000, 0)
	limiter.now = func() time.Time { return at }
	bid := uniqueBroadcaster()
	for range maxLightsPerMinute {
		ok, err := limiter.AllowLights(ctx, bid)
		require.NoError(t, err)
		assert.True(t, ok)
	}
	ok, err := limiter.AllowLights(ctx, bid)
	require.NoError(t, err)
	assert.False(t, ok)

	at = at.Add(time.Minute)
	ok, err = limiter.AllowLights(ctx, bid)
	require.NoError(t, err)
	assert.True(t, ok, "the next window starts over")
}

func TestValkeyRunLogAndLimiterFailOpen(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	client.Close()
	bid := uniqueBroadcaster()

	ok, err := newValkeyRunLimiter(client).AllowRun(ctx, bid)
	assert.True(t, ok)
	assert.Error(t, err)
	assert.NotPanics(t, func() { NewValkeyRunLog(client, zap.NewNop()).Record(ctx, bid, runRec("r", "a")) })
}

func TestValkeyRunLogOverflowDropsOldest(t *testing.T) {
	ctx := context.Background()
	runs := newValkeyRunLog(newHotPathTestClient(t), zap.NewNop(), 4)
	bid := uniqueBroadcaster()
	for i := 0; i < 10; i++ {
		runs.Record(ctx, bid, runRec("r"+strconv.Itoa(i), "a"))
	}
	assert.EqualValues(t, 6, runs.dropped.Load())

	go runs.run()
	t.Cleanup(runs.Close)
	got, err := runs.Recent(ctx, bid, "", 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"r9", "r8", "r7", "r6"}, runIDs(got))
}

func TestValkeyRunLogCloseFlushesQueued(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	runs := NewValkeyRunLog(client, zap.NewNop())
	bid := uniqueBroadcaster()
	for i := 0; i < 30; i++ {
		runs.Record(ctx, bid, runRec("r"+strconv.Itoa(i), "a"))
	}
	runs.Close()
	runs.Close()

	n, err := client.Do(ctx, client.B().Llen().Key(runLogKey(bid)).Build()).AsInt64()
	require.NoError(t, err)
	assert.EqualValues(t, 30, n)
}

func TestValkeyRunLogRecordAfterCloseIsDroppedNotWritten(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	runs := NewValkeyRunLog(client, zap.NewNop())
	bid := uniqueBroadcaster()
	runs.Record(ctx, bid, runRec("before", "a"))
	runs.Close()

	assert.NotPanics(t, func() { runs.Record(ctx, bid, runRec("after", "a")) })
	assert.EqualValues(t, 1, runs.dropped.Load())
	got, err := runs.Recent(ctx, bid, "", 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"before"}, runIDs(got))
}

func TestValkeyRunLogConcurrentRecordThenRecent(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	runs := NewValkeyRunLog(client, zap.NewNop())
	t.Cleanup(runs.Close)
	bid := uniqueBroadcaster()
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				runs.Record(ctx, bid, runRec("g"+strconv.Itoa(g)+"-"+strconv.Itoa(i), "a"))
			}
		}()
	}
	wg.Wait()

	got, err := runs.Recent(ctx, bid, "", 1000)
	require.NoError(t, err)
	assert.Len(t, got, automation.RunsKept)
	requirePositiveTTL(t, ctx, client, runLogKey(bid))
}

func TestValkeyRunLogBatchKeepsOrderAndTrimPerBroadcaster(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	runs := newValkeyRunLog(client, zap.NewNop(), runLogBuffer)
	first, second := uniqueBroadcaster(), uniqueBroadcaster()+1
	for i := 0; i < automation.RunsKept+10; i++ {
		runs.Record(ctx, first, runRec("a"+strconv.Itoa(i), "x"))
		if i < 3 {
			runs.Record(ctx, second, runRec("b"+strconv.Itoa(i), "y"))
		}
	}
	go runs.run()
	t.Cleanup(runs.Close)

	gotFirst, err := runs.Recent(ctx, first, "", 1000)
	require.NoError(t, err)
	require.Len(t, gotFirst, automation.RunsKept)
	assert.Equal(t, "a59", gotFirst[0].RunID)
	assert.Equal(t, "a10", gotFirst[len(gotFirst)-1].RunID)
	gotSecond, err := runs.Recent(ctx, second, "", 1000)
	require.NoError(t, err)
	assert.Equal(t, []string{"b2", "b1", "b0"}, runIDs(gotSecond))
	requirePositiveTTL(t, ctx, client, runLogKey(first))
	requirePositiveTTL(t, ctx, client, runLogKey(second))
}

func TestValkeyRunLogRecordDoesNotAllocate(t *testing.T) {
	ctx := context.Background()
	runs := newValkeyRunLog(newHotPathTestClient(t), zap.NewNop(), runLogBuffer)
	rec := runRec("r", "a")
	assert.Zero(t, testing.AllocsPerRun(2000, func() { runs.Record(ctx, 7, rec) }))
}

func fixedClock(unix int64) func() time.Time {
	return func() time.Time { return time.Unix(unix, 0) }
}

func replicaLimiters(client valkey.Client, replicas int) []*ValkeyRunLimiter {
	limiters := make([]*ValkeyRunLimiter, replicas)
	for i := range limiters {
		limiters[i] = newValkeyRunLimiter(client)
		limiters[i].now = fixedClock(1_800_000_000)
	}
	return limiters
}

func allowedRuns(ctx context.Context, limiter *ValkeyRunLimiter, bid uint64, attempts int) int {
	allowed := 0
	for i := 0; i < attempts; i++ {
		if ok, _ := limiter.AllowRun(ctx, bid); ok {
			allowed++
		}
	}
	return allowed
}

func hammer(ctx context.Context, limiter *ValkeyRunLimiter, bid uint64, allowed *atomic.Int64) {
	for i := 0; i < 500; i++ {
		if ok, err := limiter.AllowRun(ctx, bid); ok && err == nil {
			allowed.Add(1)
		}
	}
}

func TestValkeyRunLimiterNeverExceedsCapAcrossReplicas(t *testing.T) {
	ctx := context.Background()
	limiters := replicaLimiters(newHotPathTestClient(t), 8)
	for channel := 0; channel < 4; channel++ {
		bid := uniqueBroadcaster() + uint64(channel)
		var allowed atomic.Int64
		var wg sync.WaitGroup
		for _, limiter := range limiters {
			for g := 0; g < 32; g++ {
				wg.Go(func() { hammer(ctx, limiter, bid, &allowed) })
			}
		}
		wg.Wait()
		t.Logf("channel %d allowed %d of cap %d", channel, allowed.Load(), automation.MaxRunsPerMinute)
		assert.LessOrEqual(t, allowed.Load(), int64(automation.MaxRunsPerMinute))
	}
}

func TestValkeyRunLimiterSingleReplicaGetsExactlyCap(t *testing.T) {
	ctx := context.Background()
	limiter := newValkeyRunLimiter(newHotPathTestClient(t))
	limiter.now = fixedClock(1_800_000_000)
	bid := uniqueBroadcaster()
	allowed := 0
	for i := 0; i < automation.MaxRunsPerMinute+40; i++ {
		if ok, err := limiter.AllowRun(ctx, bid); ok && err == nil {
			allowed++
		}
	}
	assert.Equal(t, automation.MaxRunsPerMinute, allowed)
}

func TestValkeyRunLimiterDeniesAfterCloseWithoutValkeyCalls(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	limiter := newValkeyRunLimiter(client)
	limiter.now = fixedClock(1_800_000_000)
	bid := uniqueBroadcaster()
	for i := 0; i < automation.MaxRunsPerMinute+1; i++ {
		_, _ = limiter.AllowRun(ctx, bid)
	}
	key := runRateKey(bid, limiter.now())
	before, err := pkg_valkey.GetInt(ctx, client, key)
	require.NoError(t, err)

	for i := 0; i < 100; i++ {
		ok, err := limiter.AllowRun(ctx, bid)
		require.NoError(t, err)
		require.False(t, ok)
	}
	after, err := pkg_valkey.GetInt(ctx, client, key)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestValkeyRunLimiterRolloverDoesNotSpendOldLease(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	limiter := newValkeyRunLimiter(client)
	limiter.now = fixedClock(1_800_000_000)
	bid := uniqueBroadcaster()
	ok, err := limiter.AllowRun(ctx, bid)
	require.NoError(t, err)
	require.True(t, ok)

	limiter.now = fixedClock(1_800_000_060)
	ok, err = limiter.AllowRun(ctx, bid)
	require.NoError(t, err)
	require.True(t, ok)
	n, err := pkg_valkey.GetInt(ctx, client, runRateKey(bid, limiter.now()))
	require.NoError(t, err)
	assert.EqualValues(t, runLeaseSize, n)
}

func TestValkeyRunLimiterLocalHitDoesNotAllocate(t *testing.T) {
	ctx := context.Background()
	limiter := newValkeyRunLimiter(newHotPathTestClient(t))
	limiter.now = fixedClock(1_800_000_000)
	bid := uniqueBroadcaster()
	_, err := limiter.AllowRun(ctx, bid)
	require.NoError(t, err)
	assert.Zero(t, testing.AllocsPerRun(runLeaseSize-2, func() { _, _ = limiter.AllowRun(ctx, bid) }))
}

const leaseEpoch = 1_800_000_000

func sweepAll(ctx context.Context, limiters []*ValkeyRunLimiter, at time.Time) {
	for _, limiter := range limiters {
		limiter.sweep(ctx, at, runLeaseIdle)
	}
}

func TestValkeyRunLimiterReturnedIdleLeasesLetOneReplicaReachCap(t *testing.T) {
	ctx := context.Background()
	const replicas = 8
	bid := uniqueBroadcaster()
	limiters := replicaLimiters(newHotPathTestClient(t), replicas)
	allowed := 0
	for _, limiter := range limiters {
		allowed += allowedRuns(ctx, limiter, bid, 1)
	}
	require.Equal(t, replicas, allowed)

	later := time.Unix(leaseEpoch, 0).Add(runLeaseIdle)
	sweepAll(ctx, limiters, later)
	limiters[0].now = func() time.Time { return later }
	allowed += allowedRuns(ctx, limiters[0], bid, 2*automation.MaxRunsPerMinute)

	assert.Equal(t, automation.MaxRunsPerMinute, allowed)
}

func TestValkeyRunLimiterSweepKeepsRecentLeases(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	limiter := newValkeyRunLimiter(client)
	limiter.now = fixedClock(leaseEpoch)
	bid := uniqueBroadcaster()
	_, err := limiter.AllowRun(ctx, bid)
	require.NoError(t, err)

	limiter.sweep(ctx, time.Unix(leaseEpoch, 0).Add(runLeaseIdle-time.Second), runLeaseIdle)

	n, err := pkg_valkey.GetInt(ctx, client, runRateKey(bid, limiter.now()))
	require.NoError(t, err)
	assert.EqualValues(t, runLeaseSize, n)
}

func TestValkeyRunLimiterClosedChannelRechecksAndAdmitsReturnedSlot(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	limiters := replicaLimiters(client, 2)
	holder, spender := limiters[0], limiters[1]
	bid := uniqueBroadcaster()
	require.Equal(t, 1, allowedRuns(ctx, holder, bid, 1))
	require.Equal(t, automation.MaxRunsPerMinute-runLeaseSize, allowedRuns(ctx, spender, bid, 2*automation.MaxRunsPerMinute))

	soon := time.Unix(leaseEpoch, 0).Add(runLeaseIdle - time.Second)
	spender.now = func() time.Time { return soon }
	holder.sweep(ctx, time.Unix(leaseEpoch, 0).Add(runLeaseIdle), runLeaseIdle)
	key := runRateKey(bid, soon)
	before, err := pkg_valkey.GetInt(ctx, client, key)
	require.NoError(t, err)
	assert.Zero(t, allowedRuns(ctx, spender, bid, 5), "closed lease denies locally before the recheck")
	after, err := pkg_valkey.GetInt(ctx, client, key)
	require.NoError(t, err)
	assert.Equal(t, before, after)

	later := time.Unix(leaseEpoch, 0).Add(runLeaseIdle)
	spender.now = func() time.Time { return later }
	assert.Equal(t, runLeaseSize-1, allowedRuns(ctx, spender, bid, 2*automation.MaxRunsPerMinute))
}

func TestValkeyRunLimiterReturningLeasesNeverExceedsCapWhileSpending(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	limiters := replicaLimiters(client, 8)
	bid := uniqueBroadcaster()
	later := time.Unix(leaseEpoch, 0).Add(runLeaseIdle)
	var allowed atomic.Int64
	var spenders sync.WaitGroup
	for _, limiter := range limiters {
		for g := 0; g < 8; g++ {
			spenders.Go(func() { hammer(ctx, limiter, bid, &allowed) })
		}
	}
	stop := make(chan struct{})
	swept := make(chan struct{})
	go func() {
		defer close(swept)
		for {
			select {
			case <-stop:
				return
			default:
				sweepAll(ctx, limiters, later)
			}
		}
	}()
	spenders.Wait()
	close(stop)
	<-swept

	assert.LessOrEqual(t, allowed.Load(), int64(automation.MaxRunsPerMinute))
	n, err := pkg_valkey.GetInt(ctx, client, runRateKey(bid, time.Unix(leaseEpoch, 0)))
	require.NoError(t, err)
	assert.LessOrEqual(t, n, int64(automation.MaxRunsPerMinute))
}

func TestValkeyRunLimiterCloseReturnsCurrentWindowAndStopsSweep(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	limiter := NewValkeyRunLimiter(client)
	limiter.now = fixedClock(leaseEpoch)
	bid := uniqueBroadcaster()
	_, err := limiter.AllowRun(ctx, bid)
	require.NoError(t, err)

	limiter.Close()
	limiter.Close()

	n, err := pkg_valkey.GetInt(ctx, client, runRateKey(bid, limiter.now()))
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)
	select {
	case <-limiter.finished:
	default:
		t.Fatal("sweep goroutine still running after Close")
	}
}

func TestValkeyRunLimiterCloseReturnsNothingToOldWindow(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	limiter := NewValkeyRunLimiter(client)
	limiter.now = fixedClock(leaseEpoch)
	bid := uniqueBroadcaster()
	_, err := limiter.AllowRun(ctx, bid)
	require.NoError(t, err)

	limiter.now = fixedClock(leaseEpoch + 60)
	limiter.Close()

	n, err := pkg_valkey.GetInt(ctx, client, runRateKey(bid, time.Unix(leaseEpoch, 0)))
	require.NoError(t, err)
	assert.EqualValues(t, runLeaseSize, n)
}

func runIDs(recs []automationrpc.RunRecord) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.RunID)
	}
	return out
}
