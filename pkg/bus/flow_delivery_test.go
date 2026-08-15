package bus

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestAckIsReceiptLevelAndCostsNothing(t *testing.T) {
	sub := testFlowSubscriber()
	defer close(sub.closeCh)
	msg := deliverToLane(t, sub, laneDelivery("logical-id", []byte(`{"text":"hello"}`)))

	msg.Ack()

	// The resolve callback runs on this goroutine, so the count is already back
	// by the time Ack returns; Wait is the assertion that it was released at all.
	sub.pending.Wait()
	if sub.retried.Load() != 0 || sub.dropped.Load() != 0 {
		t.Fatalf("a receipt-level ack produced traffic: %d retries, %d drops",
			sub.retried.Load(), sub.dropped.Load())
	}
}

// TestNackRunsTheRetryPathExactlyOnce is the other half of the resolve contract.
// The verdict no longer has a goroutine of its own, so a callback invoked twice
// would schedule the event twice and one invoked never would leak the pending
// count for the whole drain window.
func TestNackRunsTheRetryPathExactlyOnce(t *testing.T) {
	sub := testFlowSubscriber()
	defer close(sub.closeCh)
	wire := laneDelivery("logical-id", []byte(`{"text":"hello"}`))
	// An exhausted budget refuses inside the shared helper, before it reaches the
	// connection, which is what lets the retry path run here without a broker.
	wire.Header.Set(RetryCountHeader, "1")
	msg := deliverToLane(t, sub, wire)

	if !msg.Nack() {
		t.Fatal("the first Nack must win")
	}
	// Losing calls must not re-enter the callback.
	msg.Nack()
	msg.Ack()

	sub.pending.Wait()
	if sub.dropped.Load() != 1 || sub.retried.Load() != 0 {
		t.Fatalf("dropped=%d retried=%d, want exactly one retry attempt",
			sub.dropped.Load(), sub.retried.Load())
	}
}

// TestADeliveryLostToShutdownReleasesItsPendingCount guards the one path where
// the resolve callback provably never runs: the send lost the race to closeCh,
// so no handler ever saw the message. Without the explicit release the count
// would sit there and shutdown would burn its whole drain budget on it.
func TestADeliveryLostToShutdownReleasesItsPendingCount(t *testing.T) {
	sub := testFlowSubscriber()
	wire := laneDelivery("logical-id", []byte(`{"text":"hello"}`))
	msg := mustFlowMessage(t, wire)

	// Nothing reads the lane channel and the binding is already closing.
	close(sub.closeCh)
	if sub.deliver(flowDelivery{wire: wire, msg: msg}) {
		t.Fatal("a delivery was accepted after the binding closed")
	}

	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	if !waitGroupBefore(&sub.pending, deadline.C) {
		t.Fatal("a delivery lost to shutdown leaked its pending count")
	}
}

// deliverToLane runs the real deliver path with a reader standing in for a
// consumer unit, and hands back the message that unit received.
func deliverToLane(t *testing.T, sub *flowSubscriber, wire *nats.Msg) *Message {
	t.Helper()
	received := make(chan *Message, 1)
	go func() { received <- <-sub.output }()

	if !sub.deliver(flowDelivery{wire: wire, msg: mustFlowMessage(t, wire)}) {
		t.Fatal("delivery was refused while the binding was open")
	}
	select {
	case msg := <-received:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("the lane never handed the message out")
		return nil
	}
}

func TestFlowSubscriberRejectsForeignSubjects(t *testing.T) {
	sub := testFlowSubscriber()
	if _, err := sub.Subscribe(context.Background(), "twitch.ingress.event.premium"); err == nil {
		t.Fatal("flow subscriber accepted a subject it is not bound to")
	}
}

func TestEveryUnitSharesOnePodLaneChannel(t *testing.T) {
	sub := testFlowSubscriber()
	first, err := sub.Subscribe(context.Background(), sub.subject)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sub.Subscribe(context.Background(), sub.subject)
	if err != nil {
		t.Fatal(err)
	}
	// One consumer per pod means one delivery stream per pod: the units compete
	// for the same channel instead of each binding its own consumer.
	if first != second {
		t.Fatal("a second consumer unit was handed its own lane channel")
	}
}
