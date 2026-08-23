// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"
)

// The pull fetch loop's own knobs: pacing defaults and the one-batch-per-loop
// lane channel contract.

func TestPullFetchLoopsDefaultsToOneAndRejectsNonPositive(t *testing.T) {
	if got := pullFetchLoops(); got != defaultPullFetchLoops {
		t.Fatalf("pullFetchLoops() = %d, want the default %d", got, defaultPullFetchLoops)
	}

	t.Setenv("NATS_PULL_FETCH_LOOPS", "4")
	if got := pullFetchLoops(); got != 4 {
		t.Fatalf("pullFetchLoops() = %d, want the override 4", got)
	}

	t.Setenv("NATS_PULL_FETCH_LOOPS", "-2")
	if got := pullFetchLoops(); got != defaultPullFetchLoops {
		t.Fatalf("a non-positive loops override was accepted: %d", got)
	}
}

// The lane channel holds every batch the fetch loops can have in flight at
// once, so a full channel is backpressure onto the server's pending set rather
// than a serialization point between the loops.
func TestPullLaneChannelHoldsOneBatchPerLoop(t *testing.T) {
	t.Setenv("NATS_PULL_FETCH_LOOPS", "3")
	t.Setenv("NATS_PULL_FETCH_BATCH", "500")

	sub := newPullSubscriber(flowLaneConfig{stream: TwitchIngressStream.Name, subject: "twitch.ingress.event.standard"},
		nil, nil, pullConsumerName("sesame", "twitch.ingress.event.standard"))
	if sub.loops != 3 || sub.batch != 500 {
		t.Fatalf("loops/batch = %d/%d, want the overrides 3/500", sub.loops, sub.batch)
	}
	if cap(sub.output) != 1500 {
		t.Fatalf("lane channel capacity = %d, want loops*batch = 1500", cap(sub.output))
	}
}
