// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

func TestPullConsumerIsOneSharedDurableForTheWholeFleet(t *testing.T) {
	t.Setenv("POD_NAME", "sesame-6d9f7c8b45-tq2xz")
	name := pullConsumerName("worker", "twitch.ingress.event.premium")

	if name != durableName("worker", "twitch.ingress.event.premium") {
		t.Fatalf("consumer name = %q, want the plain fleet-wide durable", name)
	}
	if strings.Contains(name, "tq2xz") || strings.Contains(name, podIdentity()) {
		t.Fatalf("consumer name %q carries pod identity and would fan the lane out", name)
	}
	if name == flowConsumerName("worker", "twitch.ingress.event.premium") {
		t.Fatal("pull and flow durables collide")
	}
}

func TestPullConsumerConfigIsCheapFloorAcknowledgement(t *testing.T) {
	t.Setenv("NATS_PULL_ACK_POLICY", "all")
	cfg := pullConsumerConfig("twitch.ingress.event.premium", "worker_twitch_ingress_event_premium")

	requireContract(t,
		contractClause{cfg.AckPolicy == jsapi.AckAllPolicy,
			fmt.Sprintf("ack policy = %v, want AckAll", cfg.AckPolicy)},
		contractClause{cfg.DeliverSubject == "" && !cfg.FlowControl,
			fmt.Sprintf("pull consumer was given push delivery: %#v", cfg)},
		contractClause{cfg.DeliverPolicy == jsapi.DeliverNewPolicy,
			fmt.Sprintf("deliver policy = %v, want DeliverNew on a first creation", cfg.DeliverPolicy)},
		contractClause{cfg.Replicas == defaultPullReplicas && cfg.MemoryStorage,
			fmt.Sprintf("consumer state must be replicated in memory: %#v", cfg)},
		contractClause{cfg.InactiveThreshold == flowInactiveThreshold,
			fmt.Sprintf("inactive threshold = %v, want %v", cfg.InactiveThreshold, flowInactiveThreshold)},
		contractClause{cfg.AckWait == defaultPullAckWait && cfg.MaxAckPending == defaultPullMaxAckPending,
			fmt.Sprintf("ack budget = %v/%d, want the shipped defaults", cfg.AckWait, cfg.MaxAckPending)},
	)
}

func TestPullConsumerKnobsRejectNonPositiveOverrides(t *testing.T) {
	t.Setenv("NATS_PULL_ACK_POLICY", "all")
	t.Setenv("NATS_PULL_ACK_WAIT", "45s")
	t.Setenv("NATS_PULL_MAX_ACK_PENDING", "70000")
	t.Setenv("NATS_PULL_FETCH_BATCH", "0")
	t.Setenv("NATS_PULL_FETCH_MAXWAIT", "-1s")
	t.Setenv("NATS_PULL_ACK_EVERY", "100ms")

	cfg := pullConsumerConfig("twitch.ingress.event.premium", "worker_premium")
	if cfg.AckWait != 45*time.Second || cfg.MaxAckPending != 70000 {
		t.Fatalf("valid overrides were ignored: %v/%d", cfg.AckWait, cfg.MaxAckPending)
	}
	if pullFetchBatch() != defaultPullFetchBatch || pullFetchMaxWait() != defaultPullFetchMaxWait {
		t.Fatalf("non-positive override was accepted: batch=%d wait=%v",
			pullFetchBatch(), pullFetchMaxWait())
	}
	if pullAckEvery() != 100*time.Millisecond {
		t.Fatalf("ack cadence = %v, want the override", pullAckEvery())
	}
}

func TestPullWireCarriesAStableIdentity(t *testing.T) {
	wire := fakePullDelivery(99)
	first := pullWireMessage(wire)
	second := pullWireMessage(fakePullDelivery(99))

	want := jetStreamIdentity("hub", TwitchIngressStream.Name, 99)
	if got := first.Header.Get(MessageIDHeader); got != want {
		t.Fatalf("stamped identity = %q, want %q", got, want)
	}
	if second.Header.Get(MessageIDHeader) != want {
		t.Fatal("two deliveries of the same sequence got different identities")
	}

	authored := fakePullDelivery(100)
	authored.header.Set(MessageIDHeader, "authored-id")
	if got := pullWireMessage(authored).Header.Get(MessageIDHeader); got != "authored-id" {
		t.Fatalf("publisher identity was overwritten with %q", got)
	}
}

func TestPullSubscriberIsBoundToOneSubject(t *testing.T) {
	sub := testPullSubscriber()
	defer close(sub.closeCh)

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
	if _, err := sub.Subscribe(context.Background(), "twitch.ingress.event.premium"); err == nil {
		t.Fatal("the subscriber accepted a subject it is not bound to")
	}
	sub.closed.Store(true)
	if _, err := sub.Subscribe(context.Background(), sub.subject); err == nil {
		t.Fatal("a closed subscriber handed out its lane channel")
	}
}

func TestConsumeModeIsThreeWayAndDefaultsToPull(t *testing.T) {
	for _, test := range []struct {
		name string
		flow string
		mode string
		want laneConsumeMode
	}{
		{"unset", "", "", laneModeExplicit},
		{"mode without the opt-in", "", "pull", laneModeExplicit},
		{"flow", "on", "flow", laneModeFlow},
		{"pull", "on", "pull", laneModePull},
		{"explicit", "on", "explicit", laneModeExplicit},
		{"garbage", "on", "puull", laneModePull},
		{"default", "on", "", laneModePull},
		{"backcompat off", "off", "pull", laneModeExplicit},
		{"backcompat off over flow", "off", "flow", laneModeExplicit},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("NATS_CONSUME_FLOW", test.flow)
			t.Setenv("NATS_CONSUME_MODE", test.mode)
			if got := consumeMode(); got != test.want {
				t.Fatalf("consumeMode() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPullModeStillRefusesLanesOutsideTheHotIngress(t *testing.T) {
	t.Setenv("NATS_CONSUME_FLOW", "on")
	t.Setenv("NATS_CONSUME_MODE", "pull")
	subscriber := &fleetSubscriber{group: "worker"}
	hot := subscriptionTarget{stream: TwitchIngressStandardStream.Name, topic: "twitch.ingress.event.standard"}
	control := subscriptionTarget{stream: TwitchIngressStream.Name, topic: "twitch.ingress.status.authz.revoked"}

	if got := subscriber.laneModeFor(hot); got != laneModePull {
		t.Fatalf("hot lane mode = %q, want pull", got)
	}
	if got := subscriber.laneModeFor(control); got != laneModeExplicit {
		t.Fatalf("control lane mode = %q, want explicit", got)
	}
}

func TestPullBindingReplacesThePushDurableOnTheModeFlip(t *testing.T) {
	t.Setenv("NATS_PULL_ACK_POLICY", "all")
	js := &pullConsumerSpy{live: livePushLaneConsumer(9_100)}
	name := js.live.Config.Name

	consumer, err := bindPullConsumer(
		context.Background(), js, TwitchIngressStandardStream.Name,
		pullConsumerConfig("twitch.ingress.event.standard", name),
	)
	if err != nil {
		t.Fatalf("bindPullConsumer: %v", err)
	}
	if consumer == nil {
		t.Fatal("conversion returned no consumer to fetch from")
	}
	if js.deletes != 1 {
		t.Fatalf("deletes = %d, want exactly one: the conversion is the only thing that earns a delete", js.deletes)
	}
	if len(js.created) != 1 {
		t.Fatalf("creates = %d, want one recreation", len(js.created))
	}

	got := js.created[0]
	requireContract(t,
		contractClause{got.DeliverSubject == "",
			fmt.Sprintf("replacement carries delivery subject %q; it is still a push consumer", got.DeliverSubject)},
		contractClause{got.AckPolicy == jsapi.AckAllPolicy,
			fmt.Sprintf("replacement ack policy = %v, want the pull lane's floor-based AckAll", got.AckPolicy)},
		contractClause{got.DeliverPolicy == jsapi.DeliverByStartSequencePolicy && got.OptStartSeq == 9_101,
			fmt.Sprintf("replacement resumed at %v/%d, want the predecessor's ack floor + 1",
				got.DeliverPolicy, got.OptStartSeq)},
	)
}

func TestPullReplacementNeverOpensOnTheWholeRetainedFirehose(t *testing.T) {
	js := &pullConsumerSpy{live: livePushLaneConsumer(0)}

	if _, err := bindPullConsumer(
		context.Background(), js, TwitchIngressStandardStream.Name,
		pullConsumerConfig("twitch.ingress.event.standard", js.live.Config.Name),
	); err != nil {
		t.Fatalf("bindPullConsumer: %v", err)
	}
	if got := js.created[0]; got.DeliverPolicy != jsapi.DeliverNewPolicy || got.OptStartSeq != 0 {
		t.Fatalf("unknown ack floor resumed at %v/%d, want DeliverNew", got.DeliverPolicy, got.OptStartSeq)
	}
}

func TestPullBindingBindsAConversionAnotherPodAlreadyMade(t *testing.T) {
	js := &pullConsumerSpy{live: livePushLaneConsumer(9_100), convertAfter: 2}

	consumer, err := bindPullConsumer(
		context.Background(), js, TwitchIngressStandardStream.Name,
		pullConsumerConfig("twitch.ingress.event.standard", js.live.Config.Name),
	)
	if err != nil {
		t.Fatalf("bindPullConsumer: %v", err)
	}
	if consumer == nil {
		t.Fatal("raced conversion returned no consumer to fetch from")
	}
	if js.deletes != 0 {
		t.Fatalf("deletes = %d, want none: the durable was already converted", js.deletes)
	}
}

func TestPullBindingNeverDeletesOnATransientFailure(t *testing.T) {
	js := &pullConsumerSpy{live: livePushLaneConsumer(9_100), createErr: nats.ErrNoResponders}

	_, err := bindPullConsumer(
		context.Background(), js, TwitchIngressStandardStream.Name,
		pullConsumerConfig("twitch.ingress.event.standard", js.live.Config.Name),
	)
	if err == nil {
		t.Fatal("a transient provisioning failure was swallowed")
	}
	if js.deletes != 0 {
		t.Fatalf("deletes = %d, want none: only an immutable-field rejection earns a delete", js.deletes)
	}
}
