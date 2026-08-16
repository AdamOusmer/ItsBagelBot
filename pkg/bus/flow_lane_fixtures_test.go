// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package bus

import (
	"testing"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// laneDelivery is one hot-ingress wire as the lane receives it.
func laneDelivery(id string, payload []byte) *nats.Msg {
	wire := nats.NewMsg("twitch.ingress.event.standard")
	wire.Header.Set(MessageIDHeader, id)
	wire.Data = payload
	return wire
}

func mustFlowMessage(t *testing.T, wire *nats.Msg) *Message {
	t.Helper()
	msg, err := messageFromNATS(wire)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func testFlowSubscriber() *flowSubscriber {
	return &flowSubscriber{
		stream: TwitchIngressStream.Name, subject: "twitch.ingress.event.standard",
		group: "worker", consumer: "worker_twitch_ingress_event_standard_pod_1",
		log:   zap.NewNop(),
		queue: make(chan flowDelivery, flowQueueDepth),
		// Unbuffered, like the real lane: nothing reads it in these tests.
		output:  make(chan *Message),
		closeCh: make(chan struct{}),
	}
}
