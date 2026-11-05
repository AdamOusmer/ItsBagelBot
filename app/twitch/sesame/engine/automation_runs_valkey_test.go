// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestValkeyRunLogShowsOneOutcomePerRun(t *testing.T) {
	ctx := context.Background()
	runs := NewValkeyRunLog(newHotPathTestClient(t), zap.NewNop())
	bid := uniqueBroadcaster()
	fork := automationrpc.ActionRecord{Type: "parallel", Status: automationrpc.StatusOK}
	root := runRec("forked", "a")
	root.Actions = []automationrpc.ActionRecord{fork}
	path := runRec("forked", "a")
	path.Outcome = automationrpc.OutcomeFailed
	path.Actions = []automationrpc.ActionRecord{fork, {Type: "chat", Status: automationrpc.StatusFailed}}
	runs.Record(ctx, bid, root)
	runs.Record(ctx, bid, runRec("other", "a"))
	runs.Record(ctx, bid, path)

	got, err := runs.Recent(ctx, bid, "", 10)
	require.NoError(t, err)
	assert.Equal(t, []string{"forked", "other"}, runIDs(got))
	assert.Equal(t, automationrpc.OutcomeFailed, got[0].Outcome)
	assert.Equal(t, path.Actions, got[0].Actions)
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

func TestValkeyRunLogFailsOpen(t *testing.T) {
	ctx := context.Background()
	client := newHotPathTestClient(t)
	client.Close()

	assert.NotPanics(t, func() { NewValkeyRunLog(client, zap.NewNop()).Record(ctx, uniqueBroadcaster(), runRec("r", "a")) })
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

func runIDs(recs []automationrpc.RunRecord) []string {
	out := make([]string, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.RunID)
	}
	return out
}
