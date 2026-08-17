// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"errors"
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
