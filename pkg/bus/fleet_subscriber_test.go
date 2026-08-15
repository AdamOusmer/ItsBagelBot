package bus

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFleetSubscriberRejectsSubscribeAfterClose(t *testing.T) {
	subscriber := &fleetSubscriber{}
	if err := subscriber.Close(); err != nil {
		t.Fatal(err)
	}
	_, err := subscriber.Subscribe(context.Background(), "twitch.outgress.standard")
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Subscribe after Close error = %v", err)
	}
}

func TestFleetSubscriberCloseWaitsForAdmittedRegistration(t *testing.T) {
	subscriber := &fleetSubscriber{}
	requireRegistrationAdmitted(t, subscriber)

	closed := closeFleetSubscriberAsync(subscriber)
	requireSignal(t, subscriber.closeCh, "Close did not enter the closed state")
	requireCloseBlocked(t, closed)

	subscriber.registrations.Done()
	requireCloseCompleted(t, closed)
	requireRegistrationRejected(t, subscriber)
}

func TestSharedFlowLaneOutlivesASingleUnit(t *testing.T) {
	owner := &fleetSubscriber{}
	binding := testFlowSubscriber()

	lane, err := owner.rememberFlowLane("TWITCH_INGRESS|twitch.ingress.event.standard", binding)
	if err != nil {
		t.Fatal(err)
	}
	// A second consumer unit joins the pod's existing consumer instead of
	// creating one of its own, so the pod keeps a single receipt cursor.
	requireFlowLaneShared(t, owner, lane, binding)

	if err := owner.releaseFlowLane(lane); err != nil {
		t.Fatal(err)
	}
	requireFlowLaneBinding(t, owner, lane.key, true, "retiring one unit tore down the pod's lane binding")

	_ = owner.releaseFlowLane(lane) // the last unit closes the binding
	requireFlowLaneBinding(t, owner, lane.key, false, "the last unit left the binding behind")
}

// requireFlowLaneShared states the join: the second unit takes the bound lane
// itself and raises its reference count, never a lane of its own.
func requireFlowLaneShared(t *testing.T, owner *fleetSubscriber, lane *sharedFlowLane, binding Subscriber) {
	t.Helper()
	shared, surplus, err := owner.storeFlowLane(lane.key, binding)
	if err != nil || !surplus || shared != lane {
		t.Fatalf("second unit got lane=%p surplus=%v err=%v", shared, surplus, err)
	}
	if lane.refs != 2 {
		t.Fatalf("reference count = %d, want both units", lane.refs)
	}
}

// requireFlowLaneBinding states whether the pod still holds the lane binding.
func requireFlowLaneBinding(t *testing.T, owner *fleetSubscriber, key string, want bool, failure string) {
	t.Helper()
	if _, bound := owner.flowLanes[key]; bound != want {
		t.Fatal(failure)
	}
}

func requireRegistrationAdmitted(t *testing.T, subscriber *fleetSubscriber) {
	t.Helper()
	if !subscriber.beginRegistration() {
		t.Fatal("registration was unexpectedly rejected")
	}
}

func requireRegistrationRejected(t *testing.T, subscriber *fleetSubscriber) {
	t.Helper()
	if subscriber.beginRegistration() {
		t.Fatal("registration was accepted after Close")
	}
}

func closeFleetSubscriberAsync(subscriber *fleetSubscriber) <-chan error {
	closed := make(chan error, 1)
	go func() { closed <- subscriber.Close() }()
	return closed
}

func requireSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal(failure)
	}
}

func requireCloseBlocked(t *testing.T, closed <-chan error) {
	t.Helper()
	select {
	case err := <-closed:
		t.Fatalf("Close returned before registration completed: %v", err)
	default:
	}
}

func requireCloseCompleted(t *testing.T, closed <-chan error) {
	t.Helper()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not resume after registration completed")
	}
}
