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
