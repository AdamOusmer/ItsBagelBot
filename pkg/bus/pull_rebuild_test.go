// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

// The lane durable is fleet-wide and the server is allowed to delete it, so the
// binding that names it has to be able to put it back. Before this existed, a
// deleted durable meant every pod asking a name that no longer resolved, five
// times a second, until someone restarted the deployment by hand.
func TestFetchErrorRebuildsALostDurable(t *testing.T) {
	replacement := &pullConsumerHandle{info: &jsapi.ConsumerInfo{}}
	s, rebuilds := subscriberWithRebind(replacement, nil)

	if !s.noteFetchError(jsapi.ErrConsumerNotFound) {
		t.Fatal("a fetch error on a live binding must keep the pump loop running")
	}
	if *rebuilds != 1 {
		t.Fatalf("rebuild attempts = %d, want 1 after the durable went missing", *rebuilds)
	}
	if s.consumer != jsapi.Consumer(replacement) {
		t.Fatal("the pump loop is still bound to the consumer the server has deleted")
	}
}

// Only a lost durable justifies re-provisioning. Everything else is the lane
// working through something the next fetch may well survive, and rebuilding on
// it would delete and recreate a healthy fleet-wide consumer under load.
func TestOnlyALostDurableTriggersARebuild(t *testing.T) {
	transient := []error{
		jsapi.ErrConsumerLeadershipChanged,
		jsapi.ErrNoHeartbeat,
		errors.New("nats: timeout"),
	}
	for _, err := range transient {
		if consumerGone(err) {
			t.Fatalf("consumerGone(%v) = true, want false: this is not a missing durable", err)
		}
	}
	for _, err := range []error{jsapi.ErrConsumerNotFound, jsapi.ErrConsumerDeleted} {
		if !consumerGone(err) {
			t.Fatalf("consumerGone(%v) = false, want true", err)
		}
	}
}

// A rebuild that fails is left to the next fetch error — the pump loop already
// is the retry — but it must not leave the binding pointing at nothing.
func TestAFailedRebuildKeepsTheLoopRunning(t *testing.T) {
	original := &pullConsumerHandle{info: &jsapi.ConsumerInfo{}}
	s, rebuilds := subscriberWithRebind(nil, errors.New("nats: no responders"))
	s.consumer = original

	if !s.noteFetchError(jsapi.ErrConsumerDeleted) {
		t.Fatal("a failed rebuild must not stop the pump loop")
	}
	if *rebuilds != 1 {
		t.Fatalf("rebuild attempts = %d, want 1", *rebuilds)
	}
	if s.consumer != jsapi.Consumer(original) {
		t.Fatal("a failed rebuild replaced the binding with a consumer it never got")
	}
}

// Readiness has to separate an idle lane from a wedged one. A NATS connection
// check cannot: a pod that has lost its durable stays connected and consumes
// nothing, which is how sesame reported green through seven hours of silence.
func TestLaneHealthSeparatesSilenceFromFailure(t *testing.T) {
	s, _ := subscriberWithRebind(nil, errors.New("nats: no responders"))

	if !s.Healthy() {
		t.Fatal("a lane that has never failed is healthy, however long it has been idle")
	}

	// Inside the grace window an election or a reconnect must not flap readiness.
	s.errSince.Store(time.Now().Add(-laneUnhealthyAfter / 2).UnixNano())
	if !s.Healthy() {
		t.Fatal("a lane erroring for less than the grace window must still report ready")
	}

	s.errSince.Store(time.Now().Add(-2 * laneUnhealthyAfter).UnixNano())
	if s.Healthy() {
		t.Fatal("a lane stuck in its error path past the grace window must report unready")
	}

	// One good read clears it: the loop is consuming again.
	s.noteFetchProgress()
	if !s.Healthy() {
		t.Fatal("a lane that read a message again must report ready")
	}
}

// The fleet subscriber is what a service's /readyz actually holds, so the lane
// verdict has to survive the trip through it.
func TestSubscriberHealthyAggregatesTheLanes(t *testing.T) {
	sick, _ := subscriberWithRebind(nil, nil)
	sick.errSince.Store(time.Now().Add(-2 * laneUnhealthyAfter).UnixNano())
	well, _ := subscriberWithRebind(nil, nil)

	fleet := &fleetSubscriber{flowLanes: map[string]*sharedFlowLane{
		"well": {sub: well},
	}}
	if !SubscriberHealthy(fleet) {
		t.Fatal("a fleet whose only lane is healthy must report ready")
	}

	fleet.flowLanes["sick"] = &sharedFlowLane{sub: sick}
	if SubscriberHealthy(fleet) {
		t.Fatal("one wedged lane must take the whole pod out of readiness")
	}

	// A subscriber with no lane to report on must never look sick: services that
	// do not consume lanes share this probe.
	if !SubscriberHealthy(&fleetSubscriber{}) {
		t.Fatal("a subscriber with no lanes must report ready")
	}
}

// subscriberWithRebind builds a pullSubscriber whose provisioning is a counter
// rather than a broker, and returns the attempt count alongside it.
func subscriberWithRebind(replacement jsapi.Consumer, failure error) (*pullSubscriber, *int) {
	attempts := 0
	s := &pullSubscriber{subject: "twitch.ingress.event.premium", log: zap.NewNop()}
	s.rebind = func() (jsapi.Consumer, error) {
		attempts++
		if failure != nil {
			return nil, failure
		}
		return replacement, nil
	}
	return s, &attempts
}

// The tests above drive noteFetchError and consumerGone directly, which proves
// the decision logic in isolation but not that the pump goroutine itself keeps
// running and keeps delivering. The two below drive the real pump()/
// pumpIterator() loop end to end — the only way to catch a regression where the
// loop's control flow, not just its helpers, stops making progress.

// TestPumpSurvivesATransientFetchErrorAndResumesFetching is 2026-08-16's first
// link, proven through the actual goroutine: a fetch failure that is not a
// missing durable must not end the pump, and the very next attempt must
// deliver.
func TestPumpSurvivesATransientFetchErrorAndResumesFetching(t *testing.T) {
	resumed := newPumpMessagesContext(pumpResult{msg: fakePullDelivery(1)})
	consumer := &pumpConsumer{opens: []pumpOpen{
		{err: errors.New("nats: no responders")}, // election in progress, retryable
		{iter: resumed},                          // the very next attempt succeeds
	}}

	sub := testPullSubscriber()
	sub.consumer = consumer
	sub.maxWait = time.Millisecond
	var rebinds int32
	sub.rebind = func() (jsapi.Consumer, error) {
		atomic.AddInt32(&rebinds, 1)
		return nil, errors.New("must not be called")
	}

	sub.workers.Add(1)
	go sub.pump()
	defer func() {
		close(sub.closeCh)
		sub.workers.Wait()
	}()

	select {
	case msg := <-sub.output:
		if msg == nil {
			t.Fatal("pump delivered a nil message")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pump never resumed fetching after the transient error: it exited or wedged")
	}
	if atomic.LoadInt32(&rebinds) != 0 {
		t.Fatal("a transient fetch error must not provision a replacement durable")
	}
}

// TestPumpRecreatesAConsumerDeletedUnderneathItAndDeliveryResumes is the exact
// 2026-08-16 failure, driven through the real loop instead of asserted against
// noteFetchError's return value: the durable is gone, the pump must rebind to a
// replacement on its own, and the very next fetch must deliver through it —
// proving the recovery a manual kubectl rollout restart used to be the only way
// to get.
func TestPumpRecreatesAConsumerDeletedUnderneathItAndDeliveryResumes(t *testing.T) {
	replacementIter := newPumpMessagesContext(pumpResult{msg: fakePullDelivery(7)})
	replacement := &pumpConsumer{opens: []pumpOpen{{iter: replacementIter}}}

	reaped := &pumpConsumer{opens: []pumpOpen{
		{err: jsapi.ErrConsumerNotFound}, // NATS reaped it under InactiveThreshold
	}}

	sub := testPullSubscriber()
	sub.consumer = reaped
	sub.maxWait = time.Millisecond
	var rebinds int32
	sub.rebind = func() (jsapi.Consumer, error) {
		atomic.AddInt32(&rebinds, 1)
		return replacement, nil
	}

	sub.workers.Add(1)
	go sub.pump()
	defer func() {
		close(sub.closeCh)
		sub.workers.Wait()
	}()

	select {
	case msg := <-sub.output:
		if msg == nil {
			t.Fatal("pump delivered a nil message")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delivery never resumed after the durable was rebuilt")
	}
	if got := atomic.LoadInt32(&rebinds); got != 1 {
		t.Fatalf("rebind attempts = %d, want exactly 1", got)
	}
	// Safe to read without synchronization beyond the channel receive above: the
	// swap and the delivery it unblocked both happen on the pump goroutine before
	// the send, so the receive already establishes the happens-before edge.
	if sub.consumer != jsapi.Consumer(replacement) {
		t.Fatal("the pump loop is still bound to the consumer the server deleted")
	}
}

// pumpResult is one scripted outcome of a MessagesContext.Next call.
type pumpResult struct {
	msg jsapi.Msg
	err error
}

// pumpMessagesContext is a scripted jetstream.MessagesContext. Each entry is
// handed to exactly one Next call, in order; once drained, Next blocks until
// Stop, matching the real iterator's contract while a pull is outstanding and
// letting stopIteratorOnClose unpark it the same way it would in production.
type pumpMessagesContext struct {
	results chan pumpResult
	stopped chan struct{}
	once    sync.Once
}

func newPumpMessagesContext(results ...pumpResult) *pumpMessagesContext {
	ch := make(chan pumpResult, len(results))
	for _, r := range results {
		ch <- r
	}
	return &pumpMessagesContext{results: ch, stopped: make(chan struct{})}
}

func (m *pumpMessagesContext) Next(...jsapi.NextOpt) (jsapi.Msg, error) {
	select {
	case r := <-m.results:
		return r.msg, r.err
	case <-m.stopped:
		return nil, jsapi.ErrMsgIteratorClosed
	}
}

func (m *pumpMessagesContext) Stop()  { m.once.Do(func() { close(m.stopped) }) }
func (m *pumpMessagesContext) Drain() { m.Stop() }

// pumpOpen is one scripted outcome of a Consumer.Messages call: either an
// iterator to drain or the error opening one returns in isolation (the shape a
// missing durable actually fails in — Messages itself refuses before any
// iterator exists).
type pumpOpen struct {
	iter jsapi.MessagesContext
	err  error
}

// pumpConsumer hands back the next scripted open in order, repeating the last
// one once the script runs out. Embedding jsapi.Consumer(nil) means any method
// besides Messages panics if the pump loop ever starts calling it, the same
// contract pullConsumerHandle uses above.
type pumpConsumer struct {
	jsapi.Consumer
	mu    sync.Mutex
	opens []pumpOpen
	calls int
}

func (c *pumpConsumer) Messages(...jsapi.PullMessagesOpt) (jsapi.MessagesContext, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	i := c.calls
	if i >= len(c.opens) {
		i = len(c.opens) - 1
	}
	c.calls++
	return c.opens[i].iter, c.opens[i].err
}

// The tests below cover lane_coord.go: the probe that tells a peer-down R1
// consumer apart from an ordinary transient error, the KV-backed rebuild lock
// that keeps a fleet from racing its own recreate, and the floor checkpoint a
// rebuilt durable resumes from. Every scenario runs against fakeLaneCoord
// rather than a broker, the same reason pullConsumerSpy exists above.

// A probe that never answers is exactly the 2026-08-16 shape: the durable
// still resolves in meta, so consumerGone never fires, and only a bounded
// INFO call distinguishes it from a responder that is merely slow.
func TestProbeTimeoutTriggersRebuildButAProbeSuccessDoesNot(t *testing.T) {
	t.Setenv("NATS_PULL_PROBE_TIMEOUT", "20ms")

	t.Run("probe times out", func(t *testing.T) {
		replacement := &pullConsumerHandle{info: &jsapi.ConsumerInfo{}}
		s, rebuilds := subscriberWithRebind(replacement, nil)
		s.consumer = &probeConsumer{block: true}
		s.connected = func() bool { return true }

		if !s.noteFetchError(errors.New("nats: timeout")) {
			t.Fatal("a probe-triggered rebuild must keep the pump loop running")
		}
		if *rebuilds != 1 {
			t.Fatalf("rebuild attempts = %d, want 1 after the probe timed out", *rebuilds)
		}
	})

	t.Run("probe succeeds", func(t *testing.T) {
		s, rebuilds := subscriberWithRebind(nil, errors.New("must not be called"))
		s.consumer = &probeConsumer{}
		s.connected = func() bool { return true }

		if !s.noteFetchError(errors.New("nats: timeout")) {
			t.Fatal("noteFetchError must keep the pump loop running")
		}
		if *rebuilds != 0 {
			t.Fatalf("rebuild attempts = %d, want 0: the probe answered, so the fetch error was transient", *rebuilds)
		}
	})

	t.Run("not connected skips the probe entirely", func(t *testing.T) {
		s, rebuilds := subscriberWithRebind(nil, errors.New("must not be called"))
		s.consumer = &probeConsumer{block: true}
		s.connected = func() bool { return false }

		if !s.noteFetchError(errors.New("nats: timeout")) {
			t.Fatal("noteFetchError must keep the pump loop running")
		}
		if *rebuilds != 0 {
			t.Fatalf("rebuild attempts = %d, want 0: a disconnected client cannot rebuild anyway", *rebuilds)
		}
	})
}

// The lock winner performs the (floor-aware) rebuild and releases the lock
// afterward; a pod that finds a fresh lock already held skips its own
// rebuild for this cycle rather than racing the winner's create.
func TestRebuildLockWinnerRebuildsAndReleasesLoserSkips(t *testing.T) {
	coord := newFakeLaneCoord()
	name := "worker_twitch_ingress_event_premium"
	s := &pullSubscriber{subject: "twitch.ingress.event.premium", name: name, log: zap.NewNop(), coord: coord}
	replacement := &pullConsumerHandle{info: &jsapi.ConsumerInfo{}}
	var floorAttempts int
	s.floorRebind = func() (jsapi.Consumer, error) {
		floorAttempts++
		return replacement, nil
	}

	s.coordinatedRebuild()
	if floorAttempts != 1 {
		t.Fatalf("winner rebuild attempts = %d, want 1", floorAttempts)
	}
	if s.consumer != jsapi.Consumer(replacement) {
		t.Fatal("the winner must swap in the rebuilt consumer")
	}
	if _, err := coord.Get(context.Background(), laneRebuildKey(name)); !errors.Is(err, jsapi.ErrKeyNotFound) {
		t.Fatalf("winner must release the lock after rebuilding, got err=%v", err)
	}

	// Seed a fresh lock, as another pod's in-progress rebuild would, and
	// confirm this pod defers instead of racing it.
	if _, err := coord.Create(context.Background(), laneRebuildKey(name), rebuildLockValue()); err != nil {
		t.Fatalf("seed lock: %v", err)
	}
	floorAttempts = 0
	s.coordinatedRebuild()
	if floorAttempts != 0 {
		t.Fatalf("a pod must not rebuild while another pod's lock is fresh, attempts = %d", floorAttempts)
	}
}

// A lock left behind by a pod that crashed mid-rebuild must not wedge every
// other pod forever; once it is older than the stale window, the next pod to
// notice steals it via a revision-guarded update.
func TestStaleRebuildLockIsStolenViaCAS(t *testing.T) {
	t.Setenv("NATS_PULL_LOCK_STALE", "10ms")
	coord := newFakeLaneCoord()
	name := "worker_twitch_ingress_event_premium"
	stale := []byte("otherpod " + strconv.FormatInt(time.Now().Add(-time.Second).UnixNano(), 10))
	if _, err := coord.Create(context.Background(), laneRebuildKey(name), stale); err != nil {
		t.Fatalf("seed stale lock: %v", err)
	}

	s := &pullSubscriber{subject: "twitch.ingress.event.premium", name: name, log: zap.NewNop(), coord: coord}
	replacement := &pullConsumerHandle{info: &jsapi.ConsumerInfo{}}
	var floorAttempts int
	s.floorRebind = func() (jsapi.Consumer, error) {
		floorAttempts++
		return replacement, nil
	}

	s.coordinatedRebuild()
	if floorAttempts != 1 {
		t.Fatalf("a stale lock must be stolen and rebuilt, attempts = %d", floorAttempts)
	}
}

// Coordination is an optimization, never a gate: any KV error at all —
// nothing provisioned, denied, partitioned — has to fall through to the
// original lock-free rebuild rather than leave the lane stuck.
func TestKVFailureFallsBackToLockFreeRebuild(t *testing.T) {
	coord := newFakeLaneCoord()
	coord.err = errors.New("nats: no responders")

	replacement := &pullConsumerHandle{info: &jsapi.ConsumerInfo{}}
	s, rebuilds := subscriberWithRebind(replacement, nil)
	s.coord = coord

	s.coordinatedRebuild()
	if *rebuilds != 1 {
		t.Fatalf("a KV failure must fall back to the lock-free rebuild, attempts = %d", *rebuilds)
	}
}

// The rebuilt durable must resume from the fleet's floor checkpoint rather
// than replaying the retained window or, on a bare DeliverNew, silently
// skipping whatever the fleet had not yet acked.
func TestRebuildDesiredConfigUsesFloorCheckpointWhenPresent(t *testing.T) {
	name := "worker_twitch_ingress_event_premium"
	coord := newFakeLaneCoord()
	if _, err := coord.Put(context.Background(), laneFloorKey(name), []byte("41")); err != nil {
		t.Fatalf("seed floor: %v", err)
	}
	s := &pullSubscriber{name: name, coord: coord, desired: pullConsumerConfig("twitch.ingress.event.premium", name)}

	cfg := s.rebuildDesiredConfig()
	if cfg.DeliverPolicy != jsapi.DeliverByStartSequencePolicy || cfg.OptStartSeq != 42 {
		t.Fatalf("desired = %+v, want DeliverByStartSequence at seq 42", cfg)
	}
}

// With no checkpoint on record (a fresh bucket, or a lane that has never
// acked), the rebuild must fall back to the original DeliverNew rather than
// invent a start position.
func TestRebuildDesiredConfigFallsBackToDeliverNewWithoutACheckpoint(t *testing.T) {
	name := "worker_twitch_ingress_event_premium"
	s := &pullSubscriber{name: name, coord: newFakeLaneCoord(), desired: pullConsumerConfig("twitch.ingress.event.premium", name)}

	cfg := s.rebuildDesiredConfig()
	if cfg.DeliverPolicy != jsapi.DeliverNewPolicy || cfg.OptStartSeq != 0 {
		t.Fatalf("desired = %+v, want DeliverNew with no start sequence", cfg)
	}
}

// The floor checkpoint is a raft proposal on the coordination bucket's own R3
// stream, so writing one every tick regardless of progress would reintroduce,
// at tick cadence, the exact per-proposal cost the parent change removes from
// the hot path. It must write only when the acked sequence actually moved.
func TestCheckpointFloorWritesOnlyWhenTheFloorAdvanced(t *testing.T) {
	coord := newFakeLaneCoord()
	name := "worker_twitch_ingress_event_premium"
	s := &pullSubscriber{name: name, subject: "twitch.ingress.event.premium", log: zap.NewNop(), coord: coord}

	s.pending = fakePullDelivery(10)
	s.advanceFloor()
	s.checkpointFloor()
	if coord.puts != 1 {
		t.Fatalf("puts = %d, want 1 after the first advance", coord.puts)
	}
	entry, err := coord.Get(context.Background(), laneFloorKey(name))
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if string(entry.Value()) != "10" {
		t.Fatalf("checkpoint = %q, want %q", entry.Value(), "10")
	}

	// No new receipt landed: the floor has not moved, so a repeat tick must
	// not write again.
	s.checkpointFloor()
	if coord.puts != 1 {
		t.Fatalf("puts = %d, want 1: the floor had not advanced", coord.puts)
	}

	s.pending = fakePullDelivery(11)
	s.advanceFloor()
	s.checkpointFloor()
	if coord.puts != 2 {
		t.Fatalf("puts = %d, want 2 after the floor advanced again", coord.puts)
	}
}

// A local Ack failure (a dropped connection — exactly the peer-down scenario
// lane_coord.go exists for) is fire-and-forget over the wire: the broker may
// never have seen it. Checkpointing the sequence anyway would let a rebuild
// resume past a message the fleet never actually acknowledged — a skip, not
// a redelivery, and skipping is the one outcome this whole mechanism may
// never trade availability for.
func TestFailedAckNeverAdvancesTheCheckpoint(t *testing.T) {
	coord := newFakeLaneCoord()
	name := "worker_twitch_ingress_event_premium"
	s := &pullSubscriber{name: name, subject: "twitch.ingress.event.premium", log: zap.NewNop(), coord: coord}

	failed := fakePullDelivery(10)
	failed.ackErr = errors.New("nats: no responders")
	s.pending = failed
	s.advanceFloor()
	s.checkpointFloor()

	if coord.puts != 0 {
		t.Fatalf("puts = %d, want 0: the ack failed, so nothing may checkpoint", coord.puts)
	}
	if _, err := coord.Get(context.Background(), laneFloorKey(name)); !errors.Is(err, jsapi.ErrKeyNotFound) {
		t.Fatalf("floor key must not exist after a failed ack, got err=%v", err)
	}

	// A later ack that does succeed must still checkpoint normally — the
	// failure above must not have left the tracked sequence stuck.
	ok := fakePullDelivery(11)
	s.pending = ok
	s.advanceFloor()
	s.checkpointFloor()
	if coord.puts != 1 {
		t.Fatalf("puts = %d, want 1 after the next ack succeeded", coord.puts)
	}
	entry, err := coord.Get(context.Background(), laneFloorKey(name))
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	if string(entry.Value()) != "11" {
		t.Fatalf("checkpoint = %q, want %q", entry.Value(), "11")
	}
}

// A rebuild slow enough to cross pullLockStale can have its lock stolen by
// another pod while the original winner is still working. That winner's own
// release must not delete the stealer's lock: an unconditional delete would,
// reopening the lock to a third pod while the second pod is still mid-rebuild.
func TestReleaseNeverDeletesALockStolenFromUnderIt(t *testing.T) {
	coord := newFakeLaneCoord()
	name := "worker_twitch_ingress_event_premium"
	s := &pullSubscriber{subject: "twitch.ingress.event.premium", name: name, log: zap.NewNop(), coord: coord}

	lock := s.acquireRebuildLock()
	if lock.result != rebuildLockWon {
		t.Fatalf("initial acquire result = %v, want rebuildLockWon", lock.result)
	}

	// Simulate a second pod stealing the (now stale, in production) lock
	// while the first is still "working": a steal is exactly a CAS update
	// against the revision it read, landing at a revision the first pod never
	// saw.
	stolenRev, err := coord.Update(context.Background(), laneRebuildKey(name), rebuildLockValue(), lock.revision)
	if err != nil {
		t.Fatalf("simulate steal: %v", err)
	}

	// The original winner finishes and releases using its OWN (now stale)
	// revision. The CAS delete must fail and leave the stealer's lock intact.
	s.releaseRebuildLock(lock.revision)

	entry, err := coord.Get(context.Background(), laneRebuildKey(name))
	if err != nil {
		t.Fatalf("the stealer's lock must still be held, got err=%v", err)
	}
	if entry.Revision() != stolenRev {
		t.Fatalf("lock revision = %d, want the stealer's revision %d", entry.Revision(), stolenRev)
	}
}

// probeConsumer answers Info either immediately or by blocking until its
// context is done, so the probe-timeout branch in consumerUnavailable can be
// driven without a broker.
type probeConsumer struct {
	jsapi.Consumer
	block bool
	err   error
}

func (c *probeConsumer) Info(ctx context.Context) (*jsapi.ConsumerInfo, error) {
	if c.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if c.err != nil {
		return nil, c.err
	}
	return &jsapi.ConsumerInfo{}, nil
}

// fakeLaneCoordEntry is one stored key/value/revision triple.
type fakeLaneCoordEntry struct {
	value    []byte
	revision uint64
}

// fakeLaneCoord is an in-memory laneCoordinator, standing in for the KV
// bucket the same way pullConsumerSpy stands in for the stream API. Setting
// err makes every method fail uniformly, which is what the KV-unavailable
// fallback test drives.
type fakeLaneCoord struct {
	mu      sync.Mutex
	entries map[string]fakeLaneCoordEntry
	nextRev uint64
	err     error

	puts    int
	creates int
	deletes int
}

func newFakeLaneCoord() *fakeLaneCoord {
	return &fakeLaneCoord{entries: make(map[string]fakeLaneCoordEntry)}
}

func (f *fakeLaneCoord) Get(_ context.Context, key string) (jsapi.KeyValueEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	entry, ok := f.entries[key]
	if !ok {
		return nil, jsapi.ErrKeyNotFound
	}
	return &fakeKVEntry{value: entry.value, revision: entry.revision}, nil
}

func (f *fakeLaneCoord) Put(_ context.Context, key string, value []byte) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts++
	if f.err != nil {
		return 0, f.err
	}
	f.nextRev++
	f.entries[key] = fakeLaneCoordEntry{value: append([]byte(nil), value...), revision: f.nextRev}
	return f.nextRev, nil
}

func (f *fakeLaneCoord) Create(_ context.Context, key string, value []byte) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	if f.err != nil {
		return 0, f.err
	}
	if _, exists := f.entries[key]; exists {
		return 0, jsapi.ErrKeyExists
	}
	f.nextRev++
	f.entries[key] = fakeLaneCoordEntry{value: append([]byte(nil), value...), revision: f.nextRev}
	return f.nextRev, nil
}

func (f *fakeLaneCoord) Update(_ context.Context, key string, value []byte, revision uint64) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	entry, ok := f.entries[key]
	if !ok || entry.revision != revision {
		return 0, errors.New("nats: wrong last sequence")
	}
	f.nextRev++
	f.entries[key] = fakeLaneCoordEntry{value: append([]byte(nil), value...), revision: f.nextRev}
	return f.nextRev, nil
}

// Delete is revision-checked, mirroring the real KV's CAS delete
// (jsapi.LastRevision): a caller holding a stale revision — its lock having
// been stolen out from under it — must fail here, not delete whatever
// currently occupies the key.
func (f *fakeLaneCoord) Delete(_ context.Context, key string, revision uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes++
	if f.err != nil {
		return f.err
	}
	entry, ok := f.entries[key]
	if !ok {
		return jsapi.ErrKeyNotFound
	}
	if entry.revision != revision {
		return errors.New("nats: wrong last sequence")
	}
	delete(f.entries, key)
	return nil
}

// fakeKVEntry satisfies jetstream.KeyValueEntry by embedding the interface,
// so any method beyond Value/Revision panics instead of returning a zero
// value — the same convention pullConsumerHandle uses above.
type fakeKVEntry struct {
	jsapi.KeyValueEntry
	value    []byte
	revision uint64
}

func (e *fakeKVEntry) Value() []byte    { return e.value }
func (e *fakeKVEntry) Revision() uint64 { return e.revision }
