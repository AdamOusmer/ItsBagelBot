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

func TestRebuildDoesNotDeadlockWithPrimaryHandleLookup(t *testing.T) {
	s := &pullSubscriber{log: zap.NewNop()}
	rebindEntered := make(chan struct{})
	releaseRebind := make(chan struct{})
	s.rebind = func() (jsapi.Consumer, error) {
		close(rebindEntered)
		<-releaseRebind
		return &pullConsumerHandle{}, nil
	}

	rebuilt := make(chan struct{})
	go func() {
		s.rebuildConsumer()
		close(rebuilt)
	}()
	<-rebindEntered

	fetched := make(chan struct{})
	go func() {
		s.handleFor(0)
		close(fetched)
	}()

	deadline := time.Now().Add(time.Second)
	for s.handleMu.TryLock() {
		s.handleMu.Unlock()
		if time.Now().After(deadline) {
			t.Fatal("primary handle lookup never acquired handleMu")
		}
		time.Sleep(time.Millisecond)
	}
	close(releaseRebind)

	select {
	case <-rebuilt:
	case <-time.After(time.Second):
		t.Fatal("rebuild and primary handle lookup deadlocked")
	}
	select {
	case <-fetched:
	case <-time.After(time.Second):
		t.Fatal("primary handle lookup did not complete after rebuild")
	}
}

func TestLaneHealthSeparatesSilenceFromFailure(t *testing.T) {
	s, _ := subscriberWithRebind(nil, errors.New("nats: no responders"))

	if !s.Healthy() {
		t.Fatal("a lane that has never failed is healthy, however long it has been idle")
	}

	s.errSince.Store(time.Now().Add(-laneUnhealthyAfter / 2).UnixNano())
	if !s.Healthy() {
		t.Fatal("a lane erroring for less than the grace window must still report ready")
	}

	s.errSince.Store(time.Now().Add(-2 * laneUnhealthyAfter).UnixNano())
	if s.Healthy() {
		t.Fatal("a lane stuck in its error path past the grace window must report unready")
	}

	s.noteFetchProgress()
	if !s.Healthy() {
		t.Fatal("a lane that read a message again must report ready")
	}
}

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

	if !SubscriberHealthy(&fleetSubscriber{}) {
		t.Fatal("a subscriber with no lanes must report ready")
	}
}

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

func TestPumpSurvivesATransientFetchErrorAndResumesFetching(t *testing.T) {
	resumed := newPumpMessagesContext(pumpResult{msg: fakePullDelivery(1)})
	consumer := &pumpConsumer{opens: []pumpOpen{
		{err: errors.New("nats: no responders")},
		{iter: resumed},
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
	go sub.pump(0)
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

func TestPumpRecreatesAConsumerDeletedUnderneathItAndDeliveryResumes(t *testing.T) {
	replacementIter := newPumpMessagesContext(pumpResult{msg: fakePullDelivery(7)})
	replacement := &pumpConsumer{opens: []pumpOpen{{iter: replacementIter}}}

	reaped := &pumpConsumer{opens: []pumpOpen{
		{err: jsapi.ErrConsumerNotFound},
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
	go sub.pump(0)
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
	if sub.consumer != jsapi.Consumer(replacement) {
		t.Fatal("the pump loop is still bound to the consumer the server deleted")
	}
}

type pumpResult struct {
	msg jsapi.Msg
	err error
}

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

type pumpOpen struct {
	iter jsapi.MessagesContext
	err  error
}

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
