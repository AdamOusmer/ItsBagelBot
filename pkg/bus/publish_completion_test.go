// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"testing"
)

func TestPublishCompletionPreservesGapsAcrossGrowthAndWrap(t *testing.T) {
	pub := newTestBatchPublisher()
	first := pub.markAccepted()
	for range 511 {
		sequence := pub.markAccepted()
		pub.completeSequences([]uint64{sequence}, nil)
	}
	initialCapacity := len(pub.resolved)
	// Later admissions force growth while the first message still holds the
	// frontier. Results already recorded must survive the ring's relocation.
	for range 4096 {
		sequence := pub.markAccepted()
		pub.completeSequences([]uint64{sequence}, nil)
	}
	if pub.completed != 0 || len(pub.resolved) <= initialCapacity {
		t.Fatalf("frontier=%d capacity=%d, want a preserved gap and growth beyond %d",
			pub.completed, len(pub.resolved), initialCapacity)
	}
	pub.completeSequences([]uint64{first}, nil)
	if pub.completed != pub.accepted.Load() {
		t.Fatalf("growth lost a completion: frontier=%d accepted=%d", pub.completed, pub.accepted.Load())
	}

	verifyCompletionRingReuse(t, pub)
}

func verifyCompletionRingReuse(t *testing.T, pub *batchPublisher) {
	t.Helper()
	capacity := len(pub.resolved)
	// Reuse every slot multiple times with out-of-order completions. A stale
	// result from a previous lap must never advance the new lap's frontier.
	for range capacity / 16 {
		base := pub.completed
		for range 32 {
			pub.markAccepted()
		}
		for offset := uint64(32); offset > 1; offset-- {
			pub.completeSequences([]uint64{base + offset}, nil)
		}
		if pub.completed != base {
			t.Fatalf("ring reuse skipped a gap: frontier=%d want=%d", pub.completed, base)
		}
		pub.completeSequences([]uint64{base + 1}, nil)
		if pub.completed != base+32 {
			t.Fatalf("ring reuse lost completions: frontier=%d want=%d", pub.completed, base+32)
		}
	}
	if len(pub.resolved) != capacity {
		t.Fatalf("settled throughput grew the ring: capacity=%d want=%d", len(pub.resolved), capacity)
	}
}

func TestPublishFailureHistoryIsBoundedAndClearedByFlush(t *testing.T) {
	pub := newTestBatchPublisher()
	want := errors.New("broker rejected publication")
	for range maxPendingPublishErrors * 10 {
		sequence := pub.markAccepted()
		pub.completeSequences([]uint64{sequence}, want)
	}
	if len(pub.errors) > maxPendingPublishErrors {
		t.Fatalf("failure history grew to %d records", len(pub.errors))
	}
	if err := pub.Flush(context.Background()); !errors.Is(err, want) {
		t.Fatalf("Flush() = %v, want the publication failure", err)
	}
	if len(pub.errors) != 0 {
		t.Fatalf("Flush retained %d reported error records", len(pub.errors))
	}
	if err := pub.Flush(context.Background()); err != nil {
		t.Fatalf("healthy Flush repeated an old failure: %v", err)
	}
}
