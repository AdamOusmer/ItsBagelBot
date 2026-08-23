// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

// Floor-acknowledgement behaviour: the batch floor covering acked messages,
// partial drains, close-time acks, malformed deliveries and the retry path.

func TestPullFloorAckCoversTheWholeBatch(t *testing.T) {
	sub := testPullSubscriber()
	drain := drainLane(sub)

	batch := deliverPullBatch(t, sub, 1, 2, 3)
	// Nothing is acked until the batch is done: the floor is what makes the
	// per-message ack unnecessary, so a per-message ack here would be the bug.
	requireNoAcksYet(t, batch)

	sub.advanceFloor()
	requireFloorAckedOnce(t, batch)

	// The floor is published once per receipt: a second flush with nothing new
	// received must not re-ack a sequence the server already has.
	sub.advanceFloor()
	if last := batch[len(batch)-1]; last.acks() != 1 {
		t.Fatalf("floor was re-published: acks = %d", last.acks())
	}
	close(sub.closeCh)
	drain()
}

// deliverPullBatch hands the subscriber one fetch batch through the real deliver
// path, in sequence order.
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

// requireFloorAckedOnce states the cadence: one publish for the whole batch,
// naming its last message and no other.
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

// TestPullFloorAckTimerCoversAPartiallyDrainedBatch is the other half of the
// cadence. A pod blocked on a slow handler pool must not hold the shared pending
// set open for the messages it has already handed out.
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

// TestPullCloseAcksTheFinalFloor guards the shutdown path: a pod that exits
// without publishing its floor hands its whole last batch back to the fleet.
func TestPullCloseAcksTheFinalFloor(t *testing.T) {
	sub := testPullSubscriber()
	drain := drainLane(sub)

	last := fakePullDelivery(42)
	sub.deliver(last)

	// The real shutdown flushes and closes the connection; this asserts the step
	// that has to happen before it, in the order it happens.
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
	// A multi-valued header is what the decoder rejects; the delivery still
	// happened, and a hole in the floor for it would stall the shared durable at
	// MaxAckPending for every pod on the lane.
	wire.header["Bagelbot-Lane"] = []string{"premium", "standard"}

	if !sub.deliver(wire) {
		t.Fatal("a malformed delivery closed the binding")
	}
	sub.advanceFloor()
	if wire.acks() != 1 {
		t.Fatalf("malformed delivery acks = %d, want the floor to move past it", wire.acks())
	}
}

// TestPullNackUsesTheSharedRetryHelper proves the pull path reaches the same
// one-hop budget check the flow path does, rather than a second copy of it.
func TestPullNackUsesTheSharedRetryHelper(t *testing.T) {
	sub := testPullSubscriber()
	defer close(sub.closeCh)

	msg := NewMessage("event-1", []byte("{}"))
	msg.Metadata.Set(RetryCountHeader, "1")
	delivery := pullDelivery{wire: nats.NewMsg(sub.subject), msg: msg}

	// The budget is exhausted, so the helper refuses before it ever touches the
	// connection — which is also what lets this run without a broker.
	sub.scheduleRetry(delivery)
	if sub.dropped.Load() != 1 {
		t.Fatalf("dropped = %d, want the exhausted retry budget to drop the event", sub.dropped.Load())
	}
	if err := scheduleLaneRetry(nil, sub.subject, delivery.wire, msg); err == nil ||
		!strings.Contains(err.Error(), "retry budget exhausted") {
		t.Fatalf("shared helper error = %v, want the one-hop budget refusal", err)
	}
}

// TestPullNackReachesTheRetryPathFromTheResolveCallback runs the verdict through
// the real deliver path. There is no goroutine parked on the message's signals
// any more, so the callback deliver installs is the only thing that can carry a
// failure to the retry helper.
func TestPullNackReachesTheRetryPathFromTheResolveCallback(t *testing.T) {
	sub := testPullSubscriber()
	defer close(sub.closeCh)

	wire := fakePullDelivery(12)
	// An exhausted budget refuses inside the shared helper before it reaches the
	// connection, which is what lets the retry path run here without a broker.
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
	// A losing call must not schedule the event a second time.
	msg.Nack()

	sub.inflight.Wait()
	if sub.dropped.Load() != 1 || sub.retried.Load() != 0 {
		t.Fatalf("dropped=%d retried=%d, want exactly one retry attempt",
			sub.dropped.Load(), sub.retried.Load())
	}
}

// TestPullDeliveryLostToShutdownReleasesItsInflightCount guards the one path
// where the resolve callback provably never runs: the send lost the race to
// closeCh, so no handler ever saw the message. Without the explicit release,
// shutdown would spend its whole drain budget on a count nobody owns.
func TestPullDeliveryLostToShutdownReleasesItsInflightCount(t *testing.T) {
	sub := testPullSubscriber()
	// Nothing reads the lane channel and the binding is already closing.
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

// TestPullWireCarriesAStableIdentity guards the retry hop. The pull API's
// message cannot be read through nats.go's subscription-bound metadata parser,
// so without the stamp an ingress-origin event would get a fresh NUID per
// delivery and its retry would be unmatchable by any dedup guard.
