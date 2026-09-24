// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

	sub.pending.Wait()
	if sub.retried.Load() != 0 || sub.dropped.Load() != 0 {
		t.Fatalf("a receipt-level ack produced traffic: %d retries, %d drops",
			sub.retried.Load(), sub.dropped.Load())
	}
}

func TestNackRunsTheRetryPathExactlyOnce(t *testing.T) {
	sub := testFlowSubscriber()
	defer close(sub.closeCh)
	wire := laneDelivery("logical-id", []byte(`{"text":"hello"}`))
	wire.Header.Set(RetryCountHeader, "1")
	msg := deliverToLane(t, sub, wire)

	if !msg.Nack() {
		t.Fatal("the first Nack must win")
	}
	msg.Nack()
	msg.Ack()

	sub.pending.Wait()
	if sub.dropped.Load() != 1 || sub.retried.Load() != 0 {
		t.Fatalf("dropped=%d retried=%d, want exactly one retry attempt",
			sub.dropped.Load(), sub.retried.Load())
	}
}

func TestADeliveryLostToShutdownReleasesItsPendingCount(t *testing.T) {
	sub := testFlowSubscriber()
	wire := laneDelivery("logical-id", []byte(`{"text":"hello"}`))
	msg := mustFlowMessage(t, wire)

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
	if first != second {
		t.Fatal("a second consumer unit was handed its own lane channel")
	}
}
