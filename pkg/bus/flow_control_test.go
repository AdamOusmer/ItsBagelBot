// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestFlowControlResponseCarriesTheReceiptCursor(t *testing.T) {
	control := flowStatus("FlowControl Request")
	control.Reply = "$JS.FC.TWITCH_INGRESS.worker_premium.abcd"

	response := flowControlResponse(control, flowPosition{consumer: 42, stream: 99})
	if response == nil {
		t.Fatal("flow-control request produced no response")
	}
	if response.Subject != control.Reply {
		t.Fatalf("response subject = %q, want %q", response.Subject, control.Reply)
	}
	if response.Header.Get(lastConsumerHeader) != "42" || response.Header.Get(lastStreamHeader) != "99" {
		t.Fatalf("response cursor = %#v", response.Header)
	}
}

func TestIdleHeartbeatIsAnsweredOnlyWhileStalled(t *testing.T) {
	quiet := flowStatus("Idle Heartbeat")
	quiet.Header.Set(lastConsumerHeader, "500")
	quiet.Header.Set(lastStreamHeader, "900")
	if response := flowControlResponse(quiet, flowPosition{consumer: 42, stream: 99}); response != nil {
		t.Fatalf("unstalled heartbeat produced response %#v", response)
	}

	stalled := flowStatus("Idle Heartbeat")
	stalled.Header.Set(consumerStalledHeader, "$JS.FC.TWITCH_INGRESS.worker_premium.stall")
	stalled.Header.Set(lastConsumerHeader, "500")
	stalled.Header.Set(lastStreamHeader, "900")

	response := flowControlResponse(stalled, flowPosition{consumer: 42, stream: 99})
	if response == nil || response.Subject != "$JS.FC.TWITCH_INGRESS.worker_premium.stall" {
		t.Fatalf("stalled heartbeat response = %#v", response)
	}
	if response.Header.Get(lastConsumerHeader) != "42" || response.Header.Get(lastStreamHeader) != "99" {
		t.Fatalf("stalled response used the server cursor: %#v", response.Header)
	}
}

func TestFlowControlIsAnsweredBeforeAnyDelivery(t *testing.T) {
	control := flowStatus("FlowControl Request")
	control.Reply = "$JS.FC.TWITCH_INGRESS.worker_premium.abcd"

	response := flowControlResponse(control, flowPosition{})
	if response == nil {
		t.Fatal("empty cursor produced no response")
	}
	if response.Header.Get(lastStreamHeader) != "" {
		t.Fatalf("empty cursor claimed an ack floor: %#v", response.Header)
	}
}

func TestReceiptCursorTracksTheStreamSequence(t *testing.T) {
	cursor := &flowCursor{}
	if !cursor.record(10, 100) {
		t.Fatal("first sample was refused")
	}
	if cursor.record(4, 40) {
		t.Fatal("a stale sample advanced the cursor")
	}
	if got := cursor.snapshot(); got.consumer != 10 || got.stream != 100 {
		t.Fatalf("cursor regressed to %+v", got)
	}
	if !cursor.record(11, 101) || cursor.snapshot().stream != 101 {
		t.Fatalf("cursor did not advance: %+v", cursor.snapshot())
	}
}

func TestReceiptCursorSurvivesAConsumerSequenceReset(t *testing.T) {
	cursor := &flowCursor{}
	cursor.record(5_000_000, 9_000_000)

	if !cursor.record(1, 9_000_001) {
		t.Fatal("the recreated consumer's first delivery was refused")
	}
	if got := cursor.snapshot(); got.consumer != 1 || got.stream != 9_000_001 {
		t.Fatalf("cursor after reset = %+v", got)
	}

	rewound := &flowCursor{}
	rewound.record(5_000_000, 9_000_000)
	if !rewound.record(2, 12) {
		t.Fatal("a session reset below the stored stream sequence was refused")
	}
	if got := rewound.snapshot(); got.stream != 12 {
		t.Fatalf("cursor after a rewound session = %+v", got)
	}
}

func TestReceiptCursorResetsOnSelfInitiatedRecreate(t *testing.T) {
	cursor := &flowCursor{}
	cursor.record(90, 120)
	cursor.reset()
	if got := cursor.snapshot(); got != (flowPosition{}) {
		t.Fatalf("cursor after reset = %+v, want empty", got)
	}
}

func TestStatusHandlingNeverWaitsOnTheDataPath(t *testing.T) {
	sub := testFlowSubscriber()
	sub.queue = make(chan flowDelivery, 1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 4; i++ {
			sub.deliveryCallback(laneDelivery("logical-id", []byte(`{"text":"hello"}`)))
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the delivery callback blocked behind the data path")
	}
	if sub.overrun.Load() != 3 {
		t.Fatalf("overrun counter = %d, want the three refused deliveries", sub.overrun.Load())
	}
}

func TestReceiptQueueIsSizedOffTheFlowControlByteWindow(t *testing.T) {
	if flowQueueDepth != flowControlPendingBytes/flowWireBytesFloor {
		t.Fatalf("queue depth %d is no longer derived from the byte window", flowQueueDepth)
	}
	ramped := flowControlPendingBytes / 865
	if flowQueueDepth <= ramped {
		t.Fatalf("queue depth %d does not clear the ramped window of %d deliveries", flowQueueDepth, ramped)
	}
	if flowQueueDepth <= flowMaxAckPending {
		t.Fatalf("queue depth %d fell back to the message brake %d", flowQueueDepth, flowMaxAckPending)
	}
	if got := cap(testFlowSubscriber().queue); got != flowQueueDepth {
		t.Fatalf("bound lane queue = %d, want the derived depth %d", got, flowQueueDepth)
	}
}

func TestStalledHeartbeatStreakIsReportedAsAWedge(t *testing.T) {
	sub := testFlowSubscriber()

	for i := 0; i < flowWedgeStreak-1; i++ {
		sub.handleStatus(stalledFlowStatus(), controlStatus)
	}
	if sub.wedged.Load() != 0 {
		t.Fatalf("wedge reported after %d stalls, before the streak threshold", flowWedgeStreak-1)
	}

	sub.handleStatus(stalledFlowStatus(), controlStatus)
	if sub.wedged.Load() != 1 {
		t.Fatalf("wedged counter = %d after %d consecutive stalls, want 1",
			sub.wedged.Load(), flowWedgeStreak)
	}
	if sub.recovering.Load() {
		t.Fatal("a wedge triggered a consumer re-provision")
	}
}

func TestADeliveryClearsTheWedgeStreak(t *testing.T) {
	sub := testFlowSubscriber()
	for i := 0; i < flowWedgeStreak-1; i++ {
		sub.handleStatus(stalledFlowStatus(), controlStatus)
	}

	sub.deliveryCallback(laneDelivery("logical-id", []byte(`{"text":"hello"}`)))
	if got := sub.stallStreak.Load(); got != 0 {
		t.Fatalf("streak = %d after a delivery, want it cleared", got)
	}

	sub.handleStatus(stalledFlowStatus(), controlStatus)
	if sub.wedged.Load() != 0 {
		t.Fatalf("a stall after a delivery completed the old streak: wedged=%d", sub.wedged.Load())
	}
}

func TestQuietHeartbeatsNeverAccumulateAWedge(t *testing.T) {
	sub := testFlowSubscriber()
	for i := 0; i < 4*flowWedgeStreak; i++ {
		sub.handleStatus(flowStatus("Idle Heartbeat"), controlStatus)
	}
	if sub.stallStreak.Load() != 0 || sub.wedged.Load() != 0 {
		t.Fatalf("quiet heartbeats counted as stalls: streak=%d wedged=%d",
			sub.stallStreak.Load(), sub.wedged.Load())
	}
}

func TestHeartbeatSilenceIsWhatDetectsADeletedConsumer(t *testing.T) {
	now := time.Now()
	if heartbeatLost(now, now.Add(-2*time.Second).UnixNano()) {
		t.Fatal("a live one-second heartbeat was treated as silence")
	}
	if !heartbeatLost(now, now.Add(-4*time.Second).UnixNano()) {
		t.Fatal("three missed heartbeats did not trigger recovery")
	}
}

func TestStatusesOtherThanControlNeverReprovision(t *testing.T) {
	sub := testFlowSubscriber()
	deleted := flowStatus("Consumer Deleted")
	deleted.Header.Set(statusHeader, "409")
	sub.handleStatus(deleted, "409")

	if sub.recovering.Load() {
		t.Fatal("a 409 status triggered a re-provision")
	}
}

func TestDeliveryGapIsBoundedByTheAckWindow(t *testing.T) {
	if deliveriesWereLost(flowMaxAckPending, 0) {
		t.Fatal("a full in-flight window was reported as loss")
	}
	if !deliveriesWereLost(flowMaxAckPending+1, 0) {
		t.Fatal("deliveries past the ack window were not reported as lost")
	}
}

func flowStatus(description string) *nats.Msg {
	control := nats.NewMsg("_INBOX.BAGEL.worker_premium")
	control.Header.Set(statusHeader, controlStatus)
	control.Header.Set(descriptionHeader, description)
	return control
}

func stalledFlowStatus() *nats.Msg {
	stalled := flowStatus("Idle Heartbeat")
	stalled.Header.Set(consumerStalledHeader, "$JS.FC.TWITCH_INGRESS.worker_standard.stall")
	return stalled
}
