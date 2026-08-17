// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"errors"
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
