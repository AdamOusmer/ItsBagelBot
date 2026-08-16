// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package bus

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestReplaceConsumerCarriesAckFloor(t *testing.T) {
	desired := laneConsumerConfig(
		"twitch.ingress.event.premium",
		"worker",
		"worker_twitch_ingress_event_premium",
		6,
	)

	// A predecessor that acked through stream seq 41 must hand the successor a
	// start at 42: DeliverAll here would replay every retained message (up to
	// MaxAge) to the whole group.
	carryAckFloor(desired, &nats.ConsumerInfo{
		AckFloor: nats.SequenceInfo{Stream: 41},
	})
	if desired.DeliverPolicy != nats.DeliverByStartSequencePolicy {
		t.Fatalf("deliver policy = %v, want by-start-sequence", desired.DeliverPolicy)
	}
	if desired.OptStartSeq != 42 {
		t.Fatalf("start seq = %d, want ack floor + 1", desired.OptStartSeq)
	}

	// No acks yet: starting from the beginning is the correct resume point.
	fresh := laneConsumerConfig("twitch.ingress.event.standard", "worker", "w", 6)
	carryAckFloor(fresh, &nats.ConsumerInfo{})
	if fresh.DeliverPolicy != nats.DeliverAllPolicy || fresh.OptStartSeq != 0 {
		t.Fatal("zero ack floor must keep the original delivery policy")
	}
}

func TestFleetSubscriberHasBoundedPacedRedelivery(t *testing.T) {
	// A plain NACK redelivers immediately; with a four-digit budget a poison
	// message used to grind the whole fleet. The budget must stay small and the
	// pacing non-zero.
	if fleetMaxRedeliveries == 0 || fleetMaxRedeliveries > 10 {
		t.Fatalf("fleet redeliveries = %d, want a small bounded budget", fleetMaxRedeliveries)
	}
	if fleetNakDelay <= 0 {
		t.Fatalf("fleet nak delay = %v, want paced redelivery", fleetNakDelay)
	}
}

func TestLaneConsumerHasBoundedDeliveryBudget(t *testing.T) {
	cfg := laneConsumerConfig(
		"twitch.outgress.premium",
		"outgress-premium",
		"outgress-premium_twitch_outgress_premium",
		4,
	)

	if cfg.MaxDeliver != 4 {
		t.Fatalf("max deliver = %d, want initial delivery plus 3 redeliveries", cfg.MaxDeliver)
	}
	// A consumer BackOff clamps AckWait to backoff[0] on the server, which
	// redelivers still-in-flight slow handlers to other replicas and duplicates
	// the job fleet-wide (the !clip reply incident). NACK pacing belongs to the
	// subscriber's per-message NakWithDelay, never to the consumer.
	if len(cfg.BackOff) != 0 {
		t.Fatalf("backoff = %v, want none: it would clamp ack wait to its first step", cfg.BackOff)
	}
	if cfg.AckWait != 4*time.Second {
		t.Fatalf("ack wait = %v, want 4s bounded by the output dedup window", cfg.AckWait)
	}
	if cfg.AckPolicy != nats.AckExplicitPolicy {
		t.Fatalf("ack policy = %v, want explicit", cfg.AckPolicy)
	}
	if cfg.DeliverGroup != "outgress-premium" {
		t.Fatalf("delivery group = %q, want shared replica queue", cfg.DeliverGroup)
	}
	if cfg.Metadata[managedConsumerMetadata] != "true" {
		t.Fatal("consumer is not marked as server-managed")
	}
}
