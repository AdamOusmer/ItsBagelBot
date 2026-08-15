package bus

import (
	"testing"

	"github.com/nats-io/nats.go"
)

func TestWorkQueueRetentionSelectsTheConfirmedAckPath(t *testing.T) {
	for _, test := range []struct {
		stream string
		want   bool
	}{
		{OutgressStream.Name, true},
		{OutgressSystemStream.Name, true},
		{TwitchIngressStream.Name, false},
		{BagelDataStream.Name, false},
		{"NOT_IN_THE_CATALOG", false},
	} {
		if got := workQueueRetention(test.stream); got != test.want {
			t.Fatalf("workQueueRetention(%q) = %v, want %v", test.stream, got, test.want)
		}
	}
}

func TestSubscriberAckModeFollowsTheStreamRetention(t *testing.T) {
	// A lost ACK on a work queue is a redelivery of work that already ran — the
	// same chat line sent twice — so those lanes confirm the acknowledgement.
	queue := newConcurrentDurableSubscriber(concurrentSubscriberConfig{stream: OutgressStream.Name})
	if !queue.ackSync {
		t.Fatal("a work-queue lane fires acknowledgements asynchronously")
	}
	// On the replay lanes the ACK only advances a cursor, and a quorum round trip
	// per message is the cost the receipt-level design exists to avoid.
	replay := newConcurrentDurableSubscriber(concurrentSubscriberConfig{stream: TwitchIngressStream.Name})
	if replay.ackSync {
		t.Fatal("a replay lane paid a confirmed acknowledgement per message")
	}
}

func TestOutgressRetentionIsStillAWorkQueue(t *testing.T) {
	// workQueueRetention reads the catalog, so this guards the assumption rather
	// than restating it: if TWITCH_OUTGRESS ever leaves work-queue retention the
	// confirmed ack path silently turns itself off.
	if OutgressStream.Retention != nats.WorkQueuePolicy {
		t.Fatalf("TWITCH_OUTGRESS retention = %v", OutgressStream.Retention)
	}
}
