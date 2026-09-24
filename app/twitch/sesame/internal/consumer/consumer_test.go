// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package consumer

import (
	"testing"

	"ItsBagelBot/pkg/bus"
)

func testConsumer() *Consumer {
	return New(nil, nil, Config{
		Lanes: Lanes{
			PremiumSubject:  "twitch.ingress.event.premium",
			StandardSubject: "twitch.ingress.event.standard",
		},
		PremiumReserve: 25,
	}, nil)
}

func TestRetryLanesAreDrainedAlongsideTheHotLanes(t *testing.T) {
	t.Setenv("NATS_CONSUME_FLOW", "on")
	lanes := testConsumer().lanes(func(*bus.Message) error { return nil })

	want := []string{
		"twitch.ingress.event.premium",
		"twitch.ingress.event.standard",
		"twitch.ingress.retry.premium",
		"twitch.ingress.retry.standard",
	}
	if len(lanes) != len(want) {
		t.Fatalf("bound %d lanes, want %d", len(lanes), len(want))
	}
	for i, subject := range want {
		if lanes[i].Subject != subject {
			t.Fatalf("lane %d = %q, want %q", i, lanes[i].Subject, subject)
		}
	}
	premiumKeepsItsReserve := lanes[0].Reserve == 25
	retryLanesHoldNoSlots := lanes[2].Reserve == 0 && lanes[3].Reserve == 0
	reservesFollowTheLanePolicy := premiumKeepsItsReserve && retryLanesHoldNoSlots
	if !reservesFollowTheLanePolicy {
		t.Fatalf("reserves = %d/%d/%d", lanes[0].Reserve, lanes[2].Reserve, lanes[3].Reserve)
	}
}

func TestRetryLanesDisappearWithoutFlowControl(t *testing.T) {
	for _, value := range []string{"", "off"} {
		t.Setenv("NATS_CONSUME_FLOW", value)
		lanes := testConsumer().lanes(func(*bus.Message) error { return nil })
		if len(lanes) != 2 {
			t.Errorf("NATS_CONSUME_FLOW=%q bound %d lanes, want the two hot lanes", value, len(lanes))
		}
	}
}
