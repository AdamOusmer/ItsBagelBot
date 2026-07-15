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
	if !subscriber.beginRegistration() {
		t.Fatal("registration was unexpectedly rejected")
	}

	closed := make(chan error, 1)
	go func() { closed <- subscriber.Close() }()

	deadline := time.Now().Add(time.Second)
	for {
		subscriber.mu.Lock()
		closing := subscriber.closed
		subscriber.mu.Unlock()
		if closing {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Close did not enter the closed state")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-closed:
		t.Fatalf("Close returned before registration completed: %v", err)
	default:
	}

	subscriber.registrations.Done()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not resume after registration completed")
	}

	if subscriber.beginRegistration() {
		t.Fatal("registration was accepted after Close")
	}
}
