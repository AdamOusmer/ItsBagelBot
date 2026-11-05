// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	automationsrpc "ItsBagelBot/internal/domain/rpc/automations"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/idempotency"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const (
	durableTestSeatLease = 100 * time.Millisecond
	durableTestSettle    = 15 * time.Second
)

var durableTestTiming = durableTiming{
	lease:     250 * time.Millisecond,
	renew:     50 * time.Millisecond,
	sweepMin:  10 * time.Millisecond,
	call:      2 * time.Second,
	closeWait: 2 * time.Second,
}

type durableRig struct {
	*queueRig
	loyalty *autoLoyalty
}

func newDurableRig(t *testing.T, opts queueRigOptions, autos ...automation.Automation) *durableRig {
	t.Helper()
	client := newHotPathTestClient(t)
	t.Cleanup(func() {
		client.Do(context.Background(), client.B().Del().Key(durableReplicasKey).Build())
	})
	opts.bid = uniqueBroadcaster()
	prefix := "durable-test:" + strconv.FormatUint(opts.bid, 10) + ":"
	opts.dedup = NewEventDedup(idempotency.NewValkeyStore(client, prefix, zap.NewNop()), prefix, time.Minute, zap.NewNop())
	if opts.durable == nil {
		timing := durableTestTiming
		opts.durable = &timing
	}
	if opts.lease == 0 {
		opts.lease = durableTestSeatLease
	}
	loyalty := &autoLoyalty{}
	if opts.loyalty == nil {
		opts.loyalty = loyalty
	}
	return &durableRig{queueRig: newQueueRig(t, opts, autos...), loyalty: loyalty}
}

func (r *durableRig) kill(i int) { r.replicas[i].durable.halt() }

func (r *durableRig) assertDrained(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for _, key := range durableRecordKeys(DurableRef{BroadcasterID: r.bid})[:3] {
		left, err := r.client.Do(ctx, r.client.B().Hlen().Key(key).Build()).AsInt64()
		require.NoError(t, err)
		assert.Zero(t, left, key)
	}
}

func (r *durableRig) assertOutcomes(t *testing.T, runID string, want int) {
	t.Helper()
	time.Sleep(3 * durableTestTiming.lease)
	got := 0
	for _, rec := range r.runs.records() {
		if rec.RunID == runID {
			got++
		}
	}
	assert.Equal(t, want, got)
}

func (r *durableRig) awaitOutcomes(t *testing.T, runID string, want int) {
	t.Helper()
	require.Eventually(t, func() bool {
		got := 0
		for _, rec := range r.runs.records() {
			if rec.RunID == runID {
				got++
			}
		}
		return got >= want
	}, durableTestSettle, 2*time.Millisecond)
}

func (r *durableRig) awaitTextCount(t *testing.T, n int) []string {
	t.Helper()
	require.Eventually(t, func() bool { return len(r.texts(t)) >= n }, durableTestSettle, 2*time.Millisecond)
	return r.texts(t)
}

func (r *durableRig) uniqueTexts(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	var out []string
	for _, c := range r.pub.snapshot() {
		if seen[c.id] {
			continue
		}
		seen[c.id] = true
		for _, m := range flattenMessage(t, c.msg) {
			out = append(out, chatMessageText(t, m))
		}
	}
	return out
}

func counterSet(name string) automation.Action {
	return act("counter_set", obj{"name": name, "value": 1})
}

func counterBump(name string) automation.Action {
	return act("counter_add", obj{"name": name, "amount": 1})
}

func (l *autoLoyalty) setNames() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	names := make([]string, len(l.sets))
	for i, set := range l.sets {
		names[i], _, _ = strings.Cut(set, "/")
	}
	return names
}

func (l *autoLoyalty) bumpCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.bumps)
}

func TestDurableDelayResumesOnAnotherReplica(t *testing.T) {
	const pause = 400
	rig := newDurableRig(t, queueRigOptions{}, manual("a", chat("before"), delayMS(pause), counterSet("x"), chat("after")))
	started := time.Now()
	runID := rig.fire(t, 0, "a", "")
	require.Equal(t, []string{"before"}, rig.awaitTextCount(t, 1))
	rig.kill(0)

	rec := rig.awaitRecord(t, runID)
	assert.GreaterOrEqual(t, time.Since(started), pause*time.Millisecond, "the remaining delay still applies")
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
	assert.Equal(t, []string{"0:chat:ok:", "1:delay:ok:", "2:counter_set:ok:", "3:chat:ok:"}, statuses(rec))
	assert.Equal(t, []string{"before", "after"}, rig.texts(t))
	assert.Equal(t, []string{"x"}, rig.loyalty.setNames())
	rig.assertOutcomes(t, runID, 1)
	rig.assertDrained(t)
}

func TestDurableKeepsArgsLocalsAndEventAcrossTakeover(t *testing.T) {
	rig := newDurableRig(t, queueRigOptions{}, manual("a",
		saveLocal("kept", "{args}!"),
		delayMS(300),
		chat("{local:kept} {arg:a} {args}"),
	))
	runID := rig.fire(t, 0, "a", "alpha")
	rig.kill(0)
	rig.awaitRecord(t, runID)
	assert.Equal(t, []string{"alpha! alpha alpha"}, rig.texts(t))
}

func TestDurableLoopResumesRemainingIterations(t *testing.T) {
	rig := newDurableRig(t, queueRigOptions{}, manual("a",
		loopOf("3", chat("i {local:index}"), delayMS(120), counterBump("n")),
		counterSet("end"),
	))
	runID := rig.fire(t, 0, "a", "")
	require.Equal(t, []string{"i 1"}, rig.awaitTextCount(t, 1))
	rig.kill(0)

	rec := rig.awaitRecord(t, runID)
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
	assert.Equal(t, []string{"i 1", "i 2", "i 3"}, rig.texts(t), "chat order within the path")
	assert.Equal(t, 3, rig.loyalty.bumpCount(), "each iteration's effect once")
	assert.Equal(t, []string{"end"}, rig.loyalty.setNames())
	rig.assertOutcomes(t, runID, 1)
	rig.assertDrained(t)
}

func TestDurableNestedLoopAndBranchPositionsResume(t *testing.T) {
	live := obj{"type": "live", "params": obj{"state": "live"}}
	rig := newDurableRig(t, queueRigOptions{}, manual("a",
		loopOf("2", loopOf("2",
			act("if", obj{"condition": live, "then": []automation.Action{delayMS(60), counterBump("deep")}}),
			counterBump("after_if"),
		)),
		counterSet("end"),
	))
	runID := rig.fire(t, 0, "a", "")
	rig.kill(0)
	rig.awaitRecord(t, runID)
	assert.Equal(t, 8, rig.loyalty.bumpCount())
	assert.Equal(t, []string{"end"}, rig.loyalty.setNames())
	rig.assertDrained(t)
}

func TestDurableParallelPathsResumeAndKeepTheSeatUntilTheLast(t *testing.T) {
	fork := parallelOf(
		actions(delayMS(250), counterSet("a"), chat("a")),
		actions(delayMS(600), counterSet("b"), chat("b")),
	)
	rig := newDurableRig(t, queueRigOptions{},
		queued("p", queueName, fork, chat("root")),
		queued("next", queueName, chat("next")))
	runID := rig.fire(t, 0, "p", "")
	require.Equal(t, []string{"root"}, rig.awaitTextCount(t, 1))
	rig.fire(t, 1, "next", "")
	rig.kill(0)

	assert.Equal(t, []string{"root", "a", "b", "next"}, rig.awaitTextCount(t, 4))
	assert.ElementsMatch(t, []string{"a", "b"}, rig.loyalty.setNames())
	rig.assertOutcomes(t, runID, 3)
	rig.awaitEmptyLine(t)
	rig.assertDrained(t)
}

func TestDurableHolderSeatPassesToTheResumingReplica(t *testing.T) {
	rig := newDurableRig(t, queueRigOptions{},
		queued("hold", queueName, chat("start"), delayMS(600), counterSet("held"), chat("done")),
		queued("next", queueName, chat("next")))
	holder := rig.fire(t, 0, "hold", "")
	require.Equal(t, []string{"start"}, rig.awaitTextCount(t, 1))
	rig.fire(t, 1, "next", "")
	rig.kill(0)

	assert.Equal(t, []string{"start", "done", "next"}, rig.awaitTextCount(t, 3), "the seat must not lapse mid-run")
	assert.Equal(t, []string{"held"}, rig.loyalty.setNames())
	rig.assertOutcomes(t, holder, 1)
	rig.awaitEmptyLine(t)
	rig.assertDrained(t)
}

func TestDurableWaiterKeepsItsPlaceOnAnotherReplica(t *testing.T) {
	rig := newDurableRig(t, queueRigOptions{},
		queued("hold", queueName, delayMS(700), chat("done")),
		queued("tail", queueName, counterBump("ran"), chat("{args}")))
	rig.fire(t, 1, "hold", "")
	first := rig.fire(t, 0, "tail", "first")
	require.Eventually(t, func() bool { return len(rig.line(t)) == 2 }, durableTestSettle, time.Millisecond)
	second := rig.fire(t, 1, "tail", "second")
	require.Eventually(t, func() bool { return len(rig.line(t)) == 3 }, durableTestSettle, time.Millisecond)
	rig.kill(0)

	assert.Equal(t, []string{"done", "first", "second"}, rig.awaitTextCount(t, 3))
	assert.Equal(t, 2, rig.loyalty.bumpCount())
	rig.assertOutcomes(t, first, 1)
	rig.assertOutcomes(t, second, 1)
	rig.awaitEmptyLine(t)
	rig.assertDrained(t)
}

func TestDurableWaiterTimeoutSurvivesTakeover(t *testing.T) {
	rig := newDurableRig(t, queueRigOptions{}, queued("tail", queueName, chat("ran")))
	rig.replicas[0].queueTiming.maxWait = 400 * time.Millisecond
	rig.seatHolder(t, "holder")
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(30 * time.Millisecond):
				_, _ = rig.store.Touch(context.Background(), rig.seat("holder", queueName))
			}
		}
	}()
	runID := rig.fire(t, 0, "tail", "")
	rig.kill(0)
	rec := rig.awaitRecord(t, runID)
	assert.Equal(t, automationrpc.OutcomeLimited, rec.Outcome)
	assert.Equal(t, queueReasonTimeout, rec.Reason)
	assert.Empty(t, rig.texts(t))
	assert.Equal(t, []string{"holder"}, rig.line(t))
	rig.assertDrained(t)
}

func TestDurableEditedOrDeletedRecipeEndsAsChanged(t *testing.T) {
	flow := func(revision int64) automation.Automation {
		a := queued("a", queueName, delayMS(200), counterSet("late"), chat("late"))
		a.Revision = revision
		return a
	}
	for name, swap := range map[string]func(*swappableSource){
		"edited":  func(s *swappableSource) { s.swap(flow(2)) },
		"deleted": func(s *swappableSource) { s.swap() },
	} {
		t.Run(name, func(t *testing.T) {
			src := &swappableSource{}
			src.swap(flow(1))
			rig := newDurableRig(t, queueRigOptions{source: src})
			runID := rig.fire(t, 0, "a", "")
			swap(src)
			rig.kill(0)

			rec := rig.awaitRecord(t, runID)
			assert.Equal(t, automationrpc.OutcomeLimited, rec.Outcome)
			assert.Equal(t, durableReasonChanged, rec.Reason)
			assert.Empty(t, rig.texts(t))
			assert.Empty(t, rig.loyalty.setNames())
			rig.awaitEmptyLine(t)
			rig.assertOutcomes(t, runID, 1)
			rig.assertDrained(t)
		})
	}
}

type blockingLoyalty struct {
	*autoLoyalty
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (l *blockingLoyalty) CounterSet(ctx context.Context, set CounterAssignment) (bool, error) {
	if set.Name == "slow" {
		l.once.Do(func() { close(l.entered) })
		<-l.release
	}
	return l.autoLoyalty.CounterSet(ctx, set)
}

func TestDurableSegmentCutShortRepeatsWithoutRepeatingEffects(t *testing.T) {
	inner := &autoLoyalty{}
	loyalty := &blockingLoyalty{autoLoyalty: inner, entered: make(chan struct{}), release: make(chan struct{})}
	rig := newDurableRig(t, queueRigOptions{loyalty: loyalty}, manual("a",
		delayMS(20),
		counterSet("fast"),
		chat("mid"),
		counterSet("slow"),
		counterSet("last"),
		chat("end"),
	))
	runID := rig.fire(t, 0, "a", "")
	<-loyalty.entered
	rig.kill(0)

	rec := rig.awaitRecord(t, runID)
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
	assert.Contains(t, statuses(rec), "1:counter_set:skipped:duplicate")
	assert.Contains(t, statuses(rec), "4:counter_set:ok:", "steps after the crash point run")

	close(loyalty.release)
	require.Eventually(t, func() bool { return len(inner.setNames()) == 3 }, durableTestSettle, time.Millisecond)
	assert.ElementsMatch(t, []string{"fast", "last", "slow"}, inner.setNames())
	rig.assertOutcomes(t, runID, 1)
	assert.Equal(t, []string{"mid", "end"}, rig.uniqueTexts(t), "outgress drops a repeated replay id")
	rig.assertDrained(t)
}

func TestDurableRedeliveredEventDoesNotPauseTwice(t *testing.T) {
	rig := newDurableRig(t, queueRigOptions{}, manual("a", delayMS(150), counterBump("once"), chat("late")))
	req := rig.request("a", "")
	first := rig.replicas[0].RunNamed(context.Background(), req)
	second := rig.replicas[1].RunNamed(context.Background(), req)
	require.True(t, first.Accepted)
	require.True(t, second.Accepted)

	rig.awaitOutcomes(t, first.RunID, 1)
	rig.assertOutcomes(t, second.RunID, 0)
	assert.Equal(t, 1, rig.loyalty.bumpCount())
	assert.Equal(t, []string{"late"}, rig.texts(t))
	rig.assertDrained(t)
}

func TestDurablePanicFailsTheRunOnceInsteadOfCrashingEachReplica(t *testing.T) {
	rig := newDurableRig(t, queueRigOptions{loyalty: &panickingLoyalty{}},
		queued("boom", queueName, delayMS(10), counterSet("x")),
		queued("next", queueName, chat("next")))
	runID := rig.fire(t, 0, "boom", "")
	rig.fire(t, 1, "next", "")
	rec := rig.awaitRecord(t, runID)
	assert.Equal(t, automationrpc.OutcomeFailed, rec.Outcome)
	assert.Equal(t, "panic", rec.Reason)
	assert.Equal(t, []string{"next"}, rig.awaitTextCount(t, 1))
	rig.assertOutcomes(t, runID, 1)
	rig.assertDrained(t)
}

func TestDurableOwnerCarriesOnWhenValkeyLosesTheRecord(t *testing.T) {
	rig := newDurableRig(t, queueRigOptions{}, manual("a", delayMS(200), counterSet("x"), chat("late")))
	runID := rig.fire(t, 0, "a", "")
	keys := durableRecordKeys(DurableRef{BroadcasterID: rig.bid})[:3]
	require.NoError(t, rig.client.Do(context.Background(), rig.client.B().Del().Key(keys...).Build()).Error())

	rec := rig.awaitRecord(t, runID)
	assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
	assert.Equal(t, []string{"late"}, rig.texts(t))
	assert.Equal(t, []string{"x"}, rig.loyalty.setNames())
	rig.assertOutcomes(t, runID, 1)
}

func awaitSubscribers(t *testing.T, rig *durableRig, want int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		reply, err := rig.client.Do(context.Background(), rig.client.B().PubsubNumsub().Channel(durableWakeChannel).Build()).ToArray()
		if err != nil || len(reply) < 2 {
			return false
		}
		got, err := reply[1].AsInt64()
		return err == nil && got >= want
	}, durableTestSettle, time.Millisecond)
	// Replicas in one process share a connection, so the count cannot tell whether the second handler has joined yet.
	time.Sleep(100 * time.Millisecond)
}

func longLeaseTiming() *durableTiming {
	timing := durableTestTiming
	timing.lease = time.Minute
	timing.renew = 20 * time.Second
	return &timing
}

func TestDurableGracefulCloseHandsOffAtOnce(t *testing.T) {
	rig := newDurableRig(t, queueRigOptions{durable: longLeaseTiming(), lease: queueLongHold},
		queued("hold", queueName, delayMS(250), chat("done")),
		queued("tail", queueName, chat("tail")))
	holder := rig.fire(t, 0, "hold", "")
	tail := rig.fire(t, 0, "tail", "")
	require.Eventually(t, func() bool { return len(rig.line(t)) == 2 }, durableTestSettle, time.Millisecond)
	awaitSubscribers(t, rig, 1)
	started := time.Now()
	rig.closeReplica(0)

	assert.Equal(t, []string{"done", "tail"}, rig.awaitTextCount(t, 2))
	assert.Less(t, time.Since(started), 10*time.Second, "no wait for the one-minute lease")
	rig.assertOutcomes(t, holder, 1)
	rig.assertOutcomes(t, tail, 1)
	rig.assertDrained(t)
}

type downDurable struct {
	saves, reads, finishes atomic.Int64
}

var errDurableDown = errors.New("ERR valkey down")

func (s *downDurable) Save(context.Context, string, DurableSave) (DurableStanding, error) {
	s.saves.Add(1)
	return DurableMissing, errDurableDown
}

func (s *downDurable) Standing(context.Context, string, DurableRef) (DurableStanding, error) {
	s.reads.Add(1)
	return DurableMissing, errDurableDown
}

func (s *downDurable) Finish(context.Context, string, DurableRef, string) (DurableStanding, error) {
	s.finishes.Add(1)
	return DurableMissing, errDurableDown
}

func (s *downDurable) Renew(context.Context, string, time.Duration) error { return errDurableDown }

func (s *downDurable) Release(context.Context, string) error { return errDurableDown }

func (s *downDurable) Lapsed(context.Context) ([]string, time.Duration, error) {
	return nil, 0, errDurableDown
}

func (s *downDurable) Claim(context.Context, string, string, int) ([]DurableClaim, bool, error) {
	return nil, false, errDurableDown
}

func (s *downDurable) Wakes(context.Context, func()) error { return errDurableDown }

func downRig(t *testing.T, store DurableStore, autos ...automation.Automation) (*Pipeline, *autoRunLog, *fakePublisher, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.WarnLevel)
	pub, runs := &fakePublisher{}, &autoRunLog{}
	timing := durableTestTiming
	d := Deps{
		Proj: fakeReader{modules: map[string]projection.ModuleView{AutomationsModuleName: on()}}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: pub, Log: zap.New(core), Loyalty: &autoLoyalty{},
		Automations: AutomationDeps{Source: autoSource{list: autos}, Runs: runs, Durable: store, durableTiming: timing},
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	t.Cleanup(p.Close)
	return p, runs, pub, logs
}

func TestDurablePauseRunsInMemoryWhenValkeyIsDown(t *testing.T) {
	p, runs, pub, logs := downRig(t, &downDurable{}, manual("a", delayMS(20), chat("late")))
	reply := p.RunNamed(context.Background(), automationsrpc.RunRequest{BroadcasterID: "1", Name: "a", RequestID: "req-down"})
	require.True(t, reply.Accepted, reply.Refusal)
	require.Eventually(t, func() bool { return len(runs.records()) == 1 }, durableTestSettle, time.Millisecond)
	assert.Equal(t, automationrpc.OutcomeOK, runs.records()[0].Outcome)
	assert.Equal(t, []string{"late"}, pub.chatTexts(t))
	assert.NotZero(t, logs.FilterMessage("automations: valkey unavailable, paused run stays in memory").Len())
}

func TestDurableStoreIsUntouchedWithoutADelayOrQueue(t *testing.T) {
	store := &downDurable{}
	p, runs, _, _ := downRig(t, store, manual("plain", chat("hi"), counterSet("x")), manual("paused", delayMS(1), chat("late")))
	reply := p.RunNamed(context.Background(), automationsrpc.RunRequest{BroadcasterID: "1", Name: "plain", RequestID: "req-plain"})
	require.True(t, reply.Accepted, reply.Refusal)
	require.Len(t, runs.records(), 1)
	assert.Zero(t, store.saves.Load()+store.reads.Load()+store.finishes.Load())

	reply = p.RunNamed(context.Background(), automationsrpc.RunRequest{BroadcasterID: "1", Name: "paused", RequestID: "req-paused"})
	require.True(t, reply.Accepted, reply.Refusal)
	assert.NotZero(t, store.saves.Load(), "the counters do see a pause")
}

func TestQueuedChildOfAnUnqueuedParentWaitsDetached(t *testing.T) {
	for name, opts := range map[string]queueRigOptions{
		"durable":   {},
		"in memory": {durable: nil},
	} {
		t.Run(name, func(t *testing.T) {
			autos := []automation.Automation{
				queued("hold", queueName, delayMS(250), chat("held")),
				queued("child", queueName, chat("child")),
				manual("parent", runChild("child"), chat("parent")),
			}
			var rig *queueRig
			if name == "durable" {
				rig = newDurableRig(t, opts, autos...).queueRig
			} else {
				opts.lease = queueLongHold
				rig = newQueueRig(t, opts, autos...)
			}
			rig.fire(t, 0, "hold", "")
			parent := rig.fire(t, 1, "parent", "")
			rec := rig.awaitRecord(t, parent)
			assert.Equal(t, automationrpc.OutcomeOK, rec.Outcome)
			assert.Equal(t, []string{"0:run:ok:deferred", "1:chat:ok:"}, statuses(rec))
			assert.Equal(t, []string{"parent"}, rig.texts(t), "the parent does not wait")
			require.Eventually(t, func() bool { return len(rig.line(t)) == 0 }, durableTestSettle, 2*time.Millisecond)
			assert.Equal(t, []string{"parent", "held", "child"}, rig.awaitTexts(t, 3))
		})
	}
}

func TestQueuedChildrenOfUnqueuedParentsRunOneAtATime(t *testing.T) {
	rig := newDurableRig(t, queueRigOptions{},
		queued("child", queueName, chat("{args}"), delayMS(120), chat("{args}")),
		manual("parent", runChild("child")))
	rig.fire(t, 0, "parent", "a")
	require.Equal(t, []string{"a"}, rig.awaitTextCount(t, 1))
	rig.fire(t, 1, "parent", "b")
	assert.Equal(t, []string{"a", "a", "b", "b"}, rig.awaitTextCount(t, 4))
	rig.awaitEmptyLine(t)
	rig.assertDrained(t)
}

func TestQueuedChildWaitingOnADeadReplicaResumes(t *testing.T) {
	rig := newDurableRig(t, queueRigOptions{},
		queued("hold", queueName, delayMS(500), chat("held")),
		queued("child", queueName, chat("child")),
		manual("parent", runChild("child"), chat("parent")))
	rig.fire(t, 1, "hold", "")
	rig.fire(t, 0, "parent", "")
	require.Equal(t, []string{"parent"}, rig.awaitTextCount(t, 1))
	rig.kill(0)
	assert.Equal(t, []string{"parent", "held", "child"}, rig.awaitTextCount(t, 3))
	rig.awaitEmptyLine(t)
	rig.assertDrained(t)
	assert.Len(t, rig.runs.forAutomation("child"), 1)
}
