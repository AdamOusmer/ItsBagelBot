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
	t.Setenv("NATS_CONSUME_FLOW", "")
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
	// The retry lanes carry exceptional traffic and must not hold pool slots away
	// from live chat.
	if lanes[0].Reserve != 25 || lanes[2].Reserve != 0 || lanes[3].Reserve != 0 {
		t.Fatalf("reserves = %d/%d/%d", lanes[0].Reserve, lanes[2].Reserve, lanes[3].Reserve)
	}
}

func TestRetryLanesDisappearWithFlowControl(t *testing.T) {
	// The explicit-ACK path NAKs in place, so nothing ever schedules a retry and
	// a subscription to the retry lane would only be a consumer to maintain.
	t.Setenv("NATS_CONSUME_FLOW", "off")
	lanes := testConsumer().lanes(func(*bus.Message) error { return nil })
	if len(lanes) != 2 {
		t.Fatalf("bound %d lanes with flow control off, want the two hot lanes", len(lanes))
	}
}
