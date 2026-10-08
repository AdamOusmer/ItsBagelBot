// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (p *batchPublisher) acceptAndResolve(err error) uint64 {
	sequence := p.markAccepted()
	p.completeSequences([]uint64{sequence}, err)
	return sequence
}

func registeredFlushes(pubs ...*batchPublisher) []int {
	counts := make([]int, len(pubs))
	for i, pub := range pubs {
		pub.stateMu.Lock()
		counts[i] = len(pub.activeFlushes)
		pub.stateMu.Unlock()
	}
	return counts
}

func awaitRegisteredFlushes(t *testing.T, want int, pubs ...*batchPublisher) {
	t.Helper()
	wanted := make([]int, len(pubs))
	for i := range wanted {
		wanted[i] = want
	}
	require.Eventually(t, func() bool { return assert.ObjectsAreEqual(wanted, registeredFlushes(pubs...)) },
		time.Second, time.Millisecond, "Flush calls did not register on every publisher")
}

type flusher interface{ Flush(context.Context) error }

func flushAsync(pub flusher) <-chan error {
	done := make(chan error, 1)
	go func() { done <- pub.Flush(context.Background()) }()
	return done
}

func TestFlushWaitsForItsAdmissionsRatherThanLaterCompletion(t *testing.T) {
	pub := newTestBatchPublisher()
	first := pub.markAccepted()
	finished := flushAsync(pub)
	awaitRegisteredFlushes(t, 1, pub)

	pub.acceptAndResolve(nil)
	select {
	case err := <-finished:
		t.Fatalf("Flush returned after later completion: %v", err)
	default:
	}

	pub.completeSequences([]uint64{first}, nil)
	assert.NoError(t, <-finished)
}

func TestConcurrentFlushesBothReportTheSameFailedWindow(t *testing.T) {
	pub := newTestBatchPublisher()
	sequence := pub.markAccepted()
	want := errors.New("cohort aborted")
	results := []<-chan error{flushAsync(pub), flushAsync(pub)}
	awaitRegisteredFlushes(t, 2, pub)

	pub.completeSequences([]uint64{sequence}, want)

	for _, result := range results {
		assert.ErrorIs(t, <-result, want)
	}
	pub.acceptAndResolve(nil)
	assert.NoError(t, pub.Flush(context.Background()), "a healthy Flush after reporting the failure")
}

func TestPublishCompletionPreservesGapsAcrossGrowthAndWrap(t *testing.T) {
	pub := newTestBatchPublisher()
	first := pub.markAccepted()
	for range 511 {
		pub.acceptAndResolve(nil)
	}
	initialCapacity := len(pub.resolved)
	for range 4096 {
		pub.acceptAndResolve(nil)
	}
	require.Zero(t, pub.completed, "the unresolved first sequence must hold the frontier")
	require.Greater(t, len(pub.resolved), initialCapacity, "the ring must grow around the gap")

	pub.completeSequences([]uint64{first}, nil)

	assert.Equal(t, pub.accepted.Load(), pub.completed, "growth lost a completion")
	assertCompletionRingReuse(t, pub)
}

func assertCompletionRingReuse(t *testing.T, pub *batchPublisher) {
	t.Helper()
	capacity := len(pub.resolved)
	for range capacity / 16 {
		base := pub.completed
		for range 32 {
			pub.markAccepted()
		}
		for offset := uint64(32); offset > 1; offset-- {
			pub.completeSequences([]uint64{base + offset}, nil)
		}
		require.Equal(t, base, pub.completed, "ring reuse skipped a gap")
		pub.completeSequences([]uint64{base + 1}, nil)
		require.Equal(t, base+32, pub.completed, "ring reuse lost completions")
	}
	assert.Len(t, pub.resolved, capacity, "settled throughput must not grow the ring")
}

func TestPublishFailureHistoryIsBoundedAndClearedByFlush(t *testing.T) {
	pub := newTestBatchPublisher()
	want := errors.New("broker rejected publication")
	for range maxPendingPublishErrors * 10 {
		pub.acceptAndResolve(want)
	}
	require.LessOrEqual(t, len(pub.errors), maxPendingPublishErrors)

	require.ErrorIs(t, pub.Flush(context.Background()), want)

	assert.Empty(t, pub.errors, "Flush must drop the error records it reported")
	assert.NoError(t, pub.Flush(context.Background()), "a healthy Flush must not repeat an old failure")
}

func TestFlushRegressionCancelledAdmissionResolvesItsReservation(t *testing.T) {
	pub := newTestBatchPublisher()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := pub.admitLocked(ctx, &publishBatchWorker{requests: make(chan publishRequest)}, publishRequest{})

	require.ErrorIs(t, err, context.Canceled)
	select {
	case err := <-flushAsync(pub):
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("cancelled admission left an unresolved Flush reservation")
	}
}

func TestFlushRegressionCancelledFlushLeavesFailureForLaterCaller(t *testing.T) {
	pub := newTestBatchPublisher()
	sequence := pub.markAccepted()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, pub.Flush(ctx), context.Canceled)

	want := errors.New("broker rejected cohort")
	pub.completeSequences([]uint64{sequence}, want)

	assert.ErrorIs(t, pub.Flush(context.Background()), want, "the failure must be preserved for a later caller")
}

func TestFlushRegressionNewHealthyFlushDoesNotInheritOldOverlapFailure(t *testing.T) {
	pub := newTestBatchPublisher()
	ctx := context.Background()
	failed := pub.markAccepted()
	firstID := pub.registerFlush(failed)
	healthy := pub.markAccepted()
	oldID := pub.registerFlush(healthy)
	want := errors.New("old cohort failed")
	pub.completeSequences([]uint64{failed}, want)
	require.ErrorIs(t, pub.waitFlush(ctx, failed, firstID), want, "the first overlapping Flush sees the old failure")

	newID := pub.registerFlush(healthy)
	pub.completeSequences([]uint64{healthy}, nil)

	assert.NoError(t, pub.waitFlush(ctx, healthy, newID), "a new healthy Flush during an older overlap")
	assert.ErrorIs(t, pub.waitFlush(ctx, healthy, oldID), want, "the older overlapping Flush keeps its failure")
}

func TestFlushRegressionPoolWaitsForEveryMemberAfterFailure(t *testing.T) {
	first, second := newTestBatchPublisher(), newTestBatchPublisher()
	firstSequence, secondSequence := first.markAccepted(), second.markAccepted()
	pool := &publisherPool{members: []*batchPublisher{first, second}}
	done := flushAsync(pool)
	awaitRegisteredFlushes(t, 1, first, second)

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
		assert.ErrorIs(t, err, want)
	case <-time.After(time.Second):
		t.Fatal("pool Flush() did not finish after every member settled")
	}
}

func TestPublishQueueSizeIsBounded(t *testing.T) {
	t.Setenv("NATS_PUBLISH_QUEUE_SIZE", "")
	assert.Equal(t, defaultPublishQueueSize, publishQueueSize())

	for value, want := range map[string]int{"1": 64, "1024": 1024, "1000000": 65_536} {
		t.Setenv("NATS_PUBLISH_QUEUE_SIZE", value)

		assert.Equal(t, want, publishQueueSize(), "NATS_PUBLISH_QUEUE_SIZE=%s", value)
	}
}
