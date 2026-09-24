// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

type laneConsumerManagerSpy struct {
	nats.JetStreamManager
	info      *nats.ConsumerInfo
	updateErr error
	updated   *nats.ConsumerConfig
	deleted   int
	added     *nats.ConsumerConfig
}

func (s *laneConsumerManagerSpy) ConsumerInfo(string, string, ...nats.JSOpt) (*nats.ConsumerInfo, error) {
	return s.info, nil
}

func (s *laneConsumerManagerSpy) UpdateConsumer(_ string, config *nats.ConsumerConfig, _ ...nats.JSOpt) (*nats.ConsumerInfo, error) {
	copy := *config
	s.updated = &copy
	return nil, s.updateErr
}

func (s *laneConsumerManagerSpy) DeleteConsumer(string, string, ...nats.JSOpt) error {
	s.deleted++
	return nil
}

func (s *laneConsumerManagerSpy) AddConsumer(_ string, config *nats.ConsumerConfig, _ ...nats.JSOpt) (*nats.ConsumerInfo, error) {
	copy := *config
	s.added = &copy
	return &nats.ConsumerInfo{Config: copy}, nil
}

func TestEnsureConsumerDoesNotReplaceAfterTransientUpdateFailure(t *testing.T) {
	spy := &laneConsumerManagerSpy{
		info:      &nats.ConsumerInfo{Config: nats.ConsumerConfig{Name: "worker", DeliverSubject: "_INBOX.existing"}},
		updateErr: context.DeadlineExceeded,
	}

	err := ensureConsumer(spy, "LANE", &nats.ConsumerConfig{Name: "worker", DeliverSubject: "_INBOX.desired"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ensureConsumer() error = %v, want deadline exceeded", err)
	}
	if spy.deleted != 0 || spy.added != nil {
		t.Fatalf("transient update failure replaced live durable: deletes=%d recreated=%v", spy.deleted, spy.added != nil)
	}
}

func TestEnsureConsumerReplacesRecognizedImmutableTransitionAtAckFloor(t *testing.T) {
	spy := &laneConsumerManagerSpy{
		info: &nats.ConsumerInfo{
			Config: nats.ConsumerConfig{
				Name:           "worker",
				DeliverSubject: "_INBOX.existing",
				DeliverPolicy:  nats.DeliverByStartSequencePolicy,
				OptStartSeq:    17,
			},
			AckFloor: nats.SequenceInfo{Stream: 41},
		},
		updateErr: errors.New("nats: ack policy can not be updated"),
	}

	if err := ensureConsumer(spy, "LANE", &nats.ConsumerConfig{Name: "worker", DeliverSubject: "_INBOX.desired"}); err != nil {
		t.Fatalf("ensureConsumer() error = %v", err)
	}
	assertLaneConsumerReplacement(t, spy)
}

func assertLaneConsumerReplacement(t *testing.T, spy *laneConsumerManagerSpy) {
	t.Helper()
	if spy.deleted != 1 || spy.added == nil {
		t.Fatalf("immutable transition deletes=%d recreated=%v, want one replacement", spy.deleted, spy.added != nil)
	}
	if spy.updated.DeliverSubject != "_INBOX.existing" || spy.updated.OptStartSeq != 17 {
		t.Fatalf("update lost legacy binding/start position: subject=%q start=%d", spy.updated.DeliverSubject, spy.updated.OptStartSeq)
	}
	if spy.added.DeliverSubject != "_INBOX.existing" {
		t.Fatalf("replacement binding = %q, want existing binding", spy.added.DeliverSubject)
	}
	if spy.added.DeliverPolicy != nats.DeliverByStartSequencePolicy || spy.added.OptStartSeq != 42 {
		t.Fatalf("replacement resumes at policy=%v start=%d, want ack floor + 1", spy.added.DeliverPolicy, spy.added.OptStartSeq)
	}
}

func TestReplaceConsumerCarriesAckFloor(t *testing.T) {
	desired := laneConsumerConfig(
		"twitch.ingress.event.premium",
		"worker",
		"worker_twitch_ingress_event_premium",
		6,
	)

	carryAckFloor(desired, &nats.ConsumerInfo{
		AckFloor: nats.SequenceInfo{Stream: 41},
	})
	if desired.DeliverPolicy != nats.DeliverByStartSequencePolicy {
		t.Fatalf("deliver policy = %v, want by-start-sequence", desired.DeliverPolicy)
	}
	if desired.OptStartSeq != 42 {
		t.Fatalf("start seq = %d, want ack floor + 1", desired.OptStartSeq)
	}

	fresh := laneConsumerConfig("twitch.ingress.event.standard", "worker", "w", 6)
	carryAckFloor(fresh, &nats.ConsumerInfo{})
	if fresh.DeliverPolicy != nats.DeliverAllPolicy || fresh.OptStartSeq != 0 {
		t.Fatal("zero ack floor must keep the original delivery policy")
	}
}

func TestFleetSubscriberHasBoundedPacedRedelivery(t *testing.T) {
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
