// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestPullFloorAckCoversTheWholeBatch(t *testing.T) {
	sub := testPullSubscriber()
	drain := drainLane(sub)

	batch := deliverPullBatch(t, sub, 1, 2, 3)
	requireNoAcksYet(t, batch)

	sub.advanceFloor()
	requireFloorAckedOnce(t, batch)

	sub.advanceFloor()
	if last := batch[len(batch)-1]; last.acks() != 1 {
		t.Fatalf("floor was re-published: acks = %d", last.acks())
	}
	close(sub.closeCh)
	drain()
}

func deliverPullBatch(t *testing.T, sub *pullSubscriber, sequences ...uint64) []*fakePullMsg {
	t.Helper()
	batch := make([]*fakePullMsg, 0, len(sequences))
	for _, sequence := range sequences {
		wire := fakePullDelivery(sequence)
		if !sub.deliver(wire) {
			t.Fatal("delivery was refused while the binding was open")
		}
		batch = append(batch, wire)
	}
	return batch
}

func requireNoAcksYet(t *testing.T, batch []*fakePullMsg) {
	t.Helper()
	for _, wire := range batch {
		if wire.acks() != 0 {
			t.Fatalf("message %d was acked individually", wire.sequence)
		}
	}
}

func requireFloorAckedOnce(t *testing.T, batch []*fakePullMsg) {
	t.Helper()
	last := batch[len(batch)-1]
	if last.acks() != 1 {
		t.Fatalf("last-of-batch acks = %d, want exactly one floor ack", last.acks())
	}
	for _, wire := range batch[:len(batch)-1] {
		if wire.acks() != 0 {
			t.Fatal("AckAll floor was published per message instead of per batch")
		}
	}
}

func TestPullFloorAckTimerCoversAPartiallyDrainedBatch(t *testing.T) {
	sub := testPullSubscriber()
	sub.ackEvery = 5 * time.Millisecond
	drain := drainLane(sub)

	first, second := fakePullDelivery(7), fakePullDelivery(8)
	sub.deliver(first)
	sub.deliver(second)

	sub.workers.Add(1)
	go sub.advanceFloorPeriodically()

	waitFor(t, func() bool { return second.acks() == 1 }, "the ack timer never advanced the floor")
	if first.acks() != 0 {
		t.Fatal("the timer acked per message instead of publishing one floor")
	}
	close(sub.closeCh)
	sub.workers.Wait()
	drain()
}

func TestPullCloseAcksTheFinalFloor(t *testing.T) {
	sub := testPullSubscriber()
	drain := drainLane(sub)

	last := fakePullDelivery(42)
	sub.deliver(last)

	close(sub.closeCh)
	sub.advanceFloor()
	if last.acks() != 1 {
		t.Fatalf("final floor acks = %d, want one", last.acks())
	}
	drain()
}

func TestPullMalformedDeliveryStillAdvancesTheFloor(t *testing.T) {
	sub := testPullSubscriber()
	defer close(sub.closeCh)

	wire := fakePullDelivery(11)
	wire.header["Bagelbot-Lane"] = []string{"premium", "standard"}

	if !sub.deliver(wire) {
		t.Fatal("a malformed delivery closed the binding")
	}
	sub.advanceFloor()
	if wire.acks() != 1 {
		t.Fatalf("malformed delivery acks = %d, want the floor to move past it", wire.acks())
	}
}

func TestPullNackUsesTheSharedRetryHelper(t *testing.T) {
	sub := testPullSubscriber()
	defer close(sub.closeCh)

	msg := NewMessage("event-1", []byte("{}"))
	msg.Metadata.Set(RetryCountHeader, "1")
	delivery := pullDelivery{wire: nats.NewMsg(sub.subject), msg: msg}

	sub.scheduleRetry(delivery)
	if sub.dropped.Load() != 1 {
		t.Fatalf("dropped = %d, want the exhausted retry budget to drop the event", sub.dropped.Load())
	}
	if err := scheduleLaneRetry(nil, sub.subject, delivery.wire, msg); err == nil ||
		!strings.Contains(err.Error(), "retry budget exhausted") {
		t.Fatalf("shared helper error = %v, want the one-hop budget refusal", err)
	}
}

func TestPullNackReachesTheRetryPathFromTheResolveCallback(t *testing.T) {
	sub := testPullSubscriber()
	defer close(sub.closeCh)

	wire := fakePullDelivery(12)
	wire.header.Set(RetryCountHeader, "1")

	received := make(chan *Message, 1)
	go func() { received <- <-sub.output }()
	if !sub.deliver(wire) {
		t.Fatal("delivery was refused while the binding was open")
	}

	msg := <-received
	if !msg.Nack() {
		t.Fatal("the first Nack must win")
	}
	msg.Nack()

	sub.inflight.Wait()
	if sub.dropped.Load() != 1 || sub.retried.Load() != 0 {
		t.Fatalf("dropped=%d retried=%d, want exactly one retry attempt",
			sub.dropped.Load(), sub.retried.Load())
	}
}

func TestPullDeliveryLostToShutdownReleasesItsInflightCount(t *testing.T) {
	sub := testPullSubscriber()
	close(sub.closeCh)

	if sub.deliver(fakePullDelivery(5)) {
		t.Fatal("a delivery was accepted after the binding closed")
	}
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	if !waitGroupBefore(&sub.inflight, deadline.C) {
		t.Fatal("a delivery lost to shutdown leaked its inflight count")
	}
}
