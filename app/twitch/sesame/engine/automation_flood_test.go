// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"runtime"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	floodMessages       = 1000
	floodPace           = 10 * time.Millisecond
	floodDelayMS        = 12_000
	floodMaxLaneStall   = time.Second
	floodGoroutineSlack = 40
)

type floodRig struct {
	p      *Pipeline
	client valkey.Client
	bid    uint64
	log    *ValkeyRunLog
}

func newFloodRig(t *testing.T, durable bool, log func(valkey.Client) *ValkeyRunLog, actions ...automation.Action) *floodRig {
	t.Helper()
	client := newHotPathTestClient(t)
	rig := &floodRig{client: client, bid: uniqueBroadcaster(), log: log(client)}
	d := Deps{
		Proj:     fakeReader{modules: map[string]projection.ModuleView{AutomationsModuleName: on()}},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      discardPublisher{},
		Log:      zap.NewNop(),
		Loyalty:  &autoLoyalty{},
		Dedup:    NewEventDedup(newRecordingStore(), "flood:", time.Minute, zap.NewNop()),
		Automations: AutomationDeps{
			Source: newCompiledSource([]automation.Automation{helloFlow(actions...)}),
			Runs:   rig.log,
		},
	}
	if durable {
		d.Automations.Durable = NewValkeyDurableRuns(client)
		t.Cleanup(func() { client.Do(context.Background(), client.B().Del().Key(durableReplicasKey).Build()) })
	}
	hook := &AutomationHook{}
	rig.p = NewPipeline(d, NewRegistry(zap.NewNop(), autoModule(hook)), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	hook.Wire(rig.p)
	t.Cleanup(rig.p.Close)
	return rig
}

func (r *floodRig) message(t *testing.T, i int) *bus.Message {
	t.Helper()
	env := lane.Envelope{
		Type: chatType, Lane: "standard", EventID: "flood-" + strconv.Itoa(i), Text: "!hello",
		BroadcasterUserID: strconv.FormatUint(r.bid, 10), ChatterUserID: "999", ChatterUserLogin: "viewer",
	}
	body, err := codec.Marshal(env)
	require.NoError(t, err)
	return bus.NewMessage(env.EventID, body)
}

type floodResult struct {
	baseline       int
	peakGoroutines int
	maxLaneStall   time.Duration
	longestRunLog  int64
}

func (r *floodRig) flood(t *testing.T, messages int, pace time.Duration) floodResult {
	t.Helper()
	res := floodResult{baseline: runtime.NumGoroutine()}
	for i := range messages {
		started := time.Now()
		require.NoError(t, r.p.Process(r.message(t, i)))
		res.maxLaneStall = max(res.maxLaneStall, time.Since(started))
		res.peakGoroutines = max(res.peakGoroutines, runtime.NumGoroutine())
		if i%100 == 0 {
			res.longestRunLog = max(res.longestRunLog, r.runLogLength(t))
		}
		time.Sleep(pace)
	}
	return res
}

func (r *floodRig) runLogLength(t *testing.T) int64 {
	t.Helper()
	n, err := r.client.Do(context.Background(), r.client.B().Llen().Key(runLogKey(r.bid)).Build()).AsInt64()
	require.NoError(t, err)
	return n
}

func startedRunLog(t *testing.T) func(valkey.Client) *ValkeyRunLog {
	return func(client valkey.Client) *ValkeyRunLog {
		log := NewValkeyRunLog(client, zap.NewNop())
		t.Cleanup(log.Close)
		return log
	}
}

func TestChatFloodRunsInlineWithABoundedRunLog(t *testing.T) {
	rig := newFloodRig(t, false, startedRunLog(t), chat("hi {user}"))
	res := rig.flood(t, floodMessages, floodPace)
	t.Logf("baseline=%d peak=%d maxLaneStall=%s longestRunLog=%d dropped=%d", res.baseline, res.peakGoroutines, res.maxLaneStall, res.longestRunLog, rig.log.dropped.Load())

	assert.LessOrEqual(t, res.peakGoroutines, res.baseline+floodGoroutineSlack)
	assert.Less(t, res.maxLaneStall, floodMaxLaneStall)
	assert.Zero(t, rig.log.dropped.Load())

	got, err := rig.log.Recent(context.Background(), rig.bid, "", automation.RunsKept)
	require.NoError(t, err)
	assert.Len(t, got, automation.RunsKept)
	assert.LessOrEqual(t, rig.runLogLength(t), int64(automation.RunsKept))
}

func TestChatFloodOfPausedRunsDrainsItsGoroutines(t *testing.T) {
	rig := newFloodRig(t, true, startedRunLog(t), chat("hi {user}"), delayMS(floodDelayMS), chat("bye"))
	res := rig.flood(t, floodMessages, floodPace)
	t.Logf("baseline=%d peak=%d perRun=%.2f maxLaneStall=%s longestRunLog=%d", res.baseline, res.peakGoroutines, float64(res.peakGoroutines-res.baseline)/floodMessages, res.maxLaneStall, res.longestRunLog)

	assert.LessOrEqual(t, res.peakGoroutines, res.baseline+floodMessages+floodGoroutineSlack)
	assert.Less(t, res.maxLaneStall, floodMaxLaneStall)
	require.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= res.baseline+floodGoroutineSlack
	}, 30*time.Second, 50*time.Millisecond, "paused runs end when their delay does")
	assert.LessOrEqual(t, rig.runLogLength(t), int64(automation.RunsKept))
}

func TestChatFloodNeverWaitsOnAStalledRunLogWriter(t *testing.T) {
	const capacity = 64
	rig := newFloodRig(t, false, func(client valkey.Client) *ValkeyRunLog {
		return newValkeyRunLog(client, zap.NewNop(), capacity)
	}, chat("hi {user}"))
	res := rig.flood(t, floodMessages, 0)
	t.Logf("maxLaneStall=%s dropped=%d", res.maxLaneStall, rig.log.dropped.Load())

	assert.Less(t, res.maxLaneStall, floodMaxLaneStall)
	assert.EqualValues(t, floodMessages-capacity, rig.log.dropped.Load())
	assert.LessOrEqual(t, rig.log.size, capacity)
}
