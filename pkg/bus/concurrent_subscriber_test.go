// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
	queue := newConcurrentDurableSubscriber(concurrentSubscriberConfig{stream: OutgressStream.Name})
	if !queue.ackSync {
		t.Fatal("a work-queue lane fires acknowledgements asynchronously")
	}
	replay := newConcurrentDurableSubscriber(concurrentSubscriberConfig{stream: TwitchIngressStream.Name})
	if replay.ackSync {
		t.Fatal("a replay lane paid a confirmed acknowledgement per message")
	}
}

func TestOutgressRetentionIsStillAWorkQueue(t *testing.T) {
	if OutgressStream.Retention != nats.WorkQueuePolicy {
		t.Fatalf("TWITCH_OUTGRESS retention = %v", OutgressStream.Retention)
	}
}
