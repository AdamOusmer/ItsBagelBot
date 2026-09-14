// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFlushRegressionCancelledAdmissionResolvesItsReservation(t *testing.T) {
	pub := newTestBatchPublisher()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := pub.admitLocked(ctx, &publishBatchWorker{requests: make(chan publishRequest)}, publishRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("admitLocked() = %v, want canceled", err)
	}

	done := make(chan error, 1)
	go func() { done <- pub.Flush(context.Background()) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Flush() after cancelled admission = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled admission left an unresolved Flush reservation")
	}
}

func TestFlushRegressionCancelledFlushLeavesFailureForLaterCaller(t *testing.T) {
	pub := newTestBatchPublisher()
	sequence := pub.markAccepted()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := pub.Flush(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Flush() = %v, want canceled", err)
	}

	want := errors.New("broker rejected cohort")
	pub.completeSequences([]uint64{sequence}, want)
	if err := pub.Flush(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Flush() after cancelled caller = %v, want preserved failure", err)
	}
}

func TestFlushRegressionNewHealthyFlushDoesNotInheritOldOverlapFailure(t *testing.T) {
	pub := newTestBatchPublisher()
	failed := pub.markAccepted()
	firstID := pub.registerFlush(failed)
	healthy := pub.markAccepted()
	oldID := pub.registerFlush(healthy)
	want := errors.New("old cohort failed")
	pub.completeSequences([]uint64{failed}, want)

	// The first caller reports the failure. That same capture preserves it for
	// the overlapping older caller, while trimming it from the history a new
	// caller will inspect.
	if err := pub.waitFlush(context.Background(), failed, firstID); !errors.Is(err, want) {
		t.Fatalf("first overlapping Flush() = %v, want old failure", err)
	}

	newID := pub.registerFlush(healthy)
	pub.completeSequences([]uint64{healthy}, nil)
	if err := pub.waitFlush(context.Background(), healthy, newID); err != nil {
		t.Fatalf("new healthy Flush() during older overlap = %v, want nil", err)
	}
	if err := pub.waitFlush(context.Background(), healthy, oldID); !errors.Is(err, want) {
		t.Fatalf("older overlapping Flush() = %v, want preserved failure", err)
	}
}

func TestFlushRegressionPoolWaitsForEveryMemberAfterFailure(t *testing.T) {
	first := newTestBatchPublisher()
	second := newTestBatchPublisher()
	firstSequence := first.markAccepted()
	secondSequence := second.markAccepted()
	pool := &publisherPool{members: []*batchPublisher{first, second}}

	done := make(chan error, 1)
	go func() { done <- pool.Flush(context.Background()) }()
	waitForFlushRegistration(t, first, second)

	want := errors.New("first member failed")
	first.completeSequences([]uint64{firstSequence}, want)
	select {
	case err := <-done:
		t.Fatalf("pool Flush() returned %v before the other member settled", err)
	case <-time.After(50 * time.Millisecond):
	}

	second.completeSequences([]uint64{secondSequence}, nil)
	select {
	case err := <-done:
		if !errors.Is(err, want) {
			t.Fatalf("pool Flush() = %v, want first member failure", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pool Flush() did not finish after every member settled")
	}
}

func waitForFlushRegistration(t *testing.T, publishers ...*batchPublisher) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		registered := true
		for _, pub := range publishers {
			pub.stateMu.Lock()
			registered = registered && len(pub.activeFlushes) != 0
			pub.stateMu.Unlock()
		}
		if registered {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("Flush did not register all publisher members")
		}
		time.Sleep(time.Millisecond)
	}
}
