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

// Pull-mode binding and lifecycle: the fleet-shared durable identity, cheap
// floor acknowledgement config, the push->pull mode flip and its conversion
// semantics across pods.

func TestPullConsumerIsOneSharedDurableForTheWholeFleet(t *testing.T) {
	t.Setenv("POD_NAME", "sesame-6d9f7c8b45-tq2xz")
	name := pullConsumerName("worker", "twitch.ingress.event.premium")

	// The absence of pod identity IS the design: every pod binding the same
	// durable is what makes the server distribute the lane instead of copying it.
	if name != durableName("worker", "twitch.ingress.event.premium") {
		t.Fatalf("consumer name = %q, want the plain fleet-wide durable", name)
	}
	if strings.Contains(name, "tq2xz") || strings.Contains(name, podIdentity()) {
		t.Fatalf("consumer name %q carries pod identity and would fan the lane out", name)
	}
	// The flow lane's per-pod name is the shape this one exists to replace.
	if name == flowConsumerName("worker", "twitch.ingress.event.premium") {
		t.Fatal("pull and flow durables collide")
	}
}

func TestPullConsumerConfigIsCheapFloorAcknowledgement(t *testing.T) {
	cfg := pullConsumerConfig("twitch.ingress.event.premium", "worker_twitch_ingress_event_premium")

	requireContract(t,
		// A floor ack costs one publish per batch; explicit acks would cost one per
		// message, which is the ack stream this lane shape exists to avoid.
		contractClause{cfg.AckPolicy == jsapi.AckAllPolicy,
			fmt.Sprintf("ack policy = %v, want AckAll", cfg.AckPolicy)},
		// A pull consumer must carry no delivery subject: one would make it a push
		// consumer and the server would stop honouring MSG.NEXT for it.
		contractClause{cfg.DeliverSubject == "" && !cfg.FlowControl,
			fmt.Sprintf("pull consumer was given push delivery: %#v", cfg)},
		contractClause{cfg.DeliverPolicy == jsapi.DeliverNewPolicy,
			fmt.Sprintf("deliver policy = %v, want DeliverNew on a first creation", cfg.DeliverPolicy)},
		// Quorum-replicated consumer state, in memory. R1 put the whole fleet's
		// ability to consume on one peer: on 2026-08-16 that peer churned, the
		// durable was lost, and every pod spun on a name that no longer resolved.
		contractClause{cfg.Replicas == defaultPullReplicas && cfg.MemoryStorage,
			fmt.Sprintf("consumer state must be replicated in memory: %#v", cfg)},
		contractClause{cfg.InactiveThreshold == flowInactiveThreshold,
			fmt.Sprintf("inactive threshold = %v, want %v", cfg.InactiveThreshold, flowInactiveThreshold)},
		contractClause{cfg.AckWait == defaultPullAckWait && cfg.MaxAckPending == defaultPullMaxAckPending,
			fmt.Sprintf("ack budget = %v/%d, want the shipped defaults", cfg.AckWait, cfg.MaxAckPending)},
	)
}

func TestPullConsumerKnobsRejectNonPositiveOverrides(t *testing.T) {
	t.Setenv("NATS_PULL_ACK_WAIT", "45s")
	t.Setenv("NATS_PULL_MAX_ACK_PENDING", "70000")
	t.Setenv("NATS_PULL_FETCH_BATCH", "0")
	t.Setenv("NATS_PULL_FETCH_MAXWAIT", "-1s")
	t.Setenv("NATS_PULL_ACK_EVERY", "100ms")

	cfg := pullConsumerConfig("twitch.ingress.event.premium", "worker_premium")
	if cfg.AckWait != 45*time.Second || cfg.MaxAckPending != 70000 {
		t.Fatalf("valid overrides were ignored: %v/%d", cfg.AckWait, cfg.MaxAckPending)
	}
	// Every knob is a rate or a ceiling, so a zero or negative one is a manifest
	// typo rather than an instruction: it must not reshape the consumer.
	if pullFetchBatch() != defaultPullFetchBatch || pullFetchMaxWait() != defaultPullFetchMaxWait {
		t.Fatalf("non-positive override was accepted: batch=%d wait=%v",
			pullFetchBatch(), pullFetchMaxWait())
	}
	if pullAckEvery() != 100*time.Millisecond {
		t.Fatalf("ack cadence = %v, want the override", pullAckEvery())
	}
}

// TestPullFloorAckCoversTheWholeBatch is the ack-cadence contract: one publish
// for every message the batch handed out, naming the last of them.
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

	// A publisher-set id is never overwritten: it is the fleet's own identity and
	// outranks the derived one.
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
	// One fetch loop per pod means one delivery stream per pod: the units compete
	// for the same channel instead of each opening its own fetch loop.
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
		// Unset is the deployed state: receipt-level modes are opt-in, so an
		// unconfigured lane keeps per-message explicit acks whatever the mode says.
		{"unset", "", "", laneModeExplicit},
		{"mode without the opt-in", "", "pull", laneModeExplicit},
		{"flow", "on", "flow", laneModeFlow},
		{"pull", "on", "pull", laneModePull},
		{"explicit", "on", "explicit", laneModeExplicit},
		// A typo must not silently reshape the lane away from the default.
		{"garbage", "on", "puull", laneModePull},
		// Unset mode with the opt-in takes the pull default: the fan-out flow
		// shape multiplies delivery per pod, pull divides it.
		{"default", "on", "", laneModePull},
		// NATS_CONSUME_FLOW=off is the deployed kill switch and outranks the mode
		// outright: an operator reaching for it must not have to know that a
		// second variable exists.
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
	// The scope guard is unchanged by the new mode: replay-sensitive lanes keep
	// per-message explicit acks whichever receipt-level mode is configured.
	if got := subscriber.laneModeFor(control); got != laneModeExplicit {
		t.Fatalf("control lane mode = %q, want explicit", got)
	}
}

// TestPullBindingReplacesThePushDurableOnTheModeFlip covers the transition the
// shared consumer name makes unavoidable. durableName(group, subject) carries no
// mode token, so flipping NATS_CONSUME_MODE to pull re-provisions the very
// durable the explicit push consumer occupies — and nats-server refuses to
// convert a push consumer to a pull one in place. Without the replacement the
// flip fails every pod's lane binding identically and the lane just stops
// consuming, which is the least visible way a lane can break.
func TestPullBindingReplacesThePushDurableOnTheModeFlip(t *testing.T) {
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
		// The fleet's acknowledged position survives the delete: resuming anywhere
		// earlier re-executes chat commands the previous consumer already handled.
		contractClause{got.DeliverPolicy == jsapi.DeliverByStartSequencePolicy && got.OptStartSeq == 9_101,
			fmt.Sprintf("replacement resumed at %v/%d, want the predecessor's ack floor + 1",
				got.DeliverPolicy, got.OptStartSeq)},
	)
}

func TestPullReplacementNeverOpensOnTheWholeRetainedFirehose(t *testing.T) {
	// A push durable that never acked anything. Inheriting its DeliverAll would
	// replay every retained event to the converted lane at once.
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

// TestPullBindingBindsAConversionAnotherPodAlreadyMade is the guard on the
// difference between the two receipt-level paths. A flow consumer is per-pod, so
// replacing one costs the caller its own cursor; this durable is fleet-wide, so
// a delete takes it out from under every other pod fetching from it. On a
// simultaneous fleet restart every pod would otherwise delete the successor the
// previous one just built.
func TestPullBindingBindsAConversionAnotherPodAlreadyMade(t *testing.T) {
	// The conversion lands between our INFO and our second update.
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
	// No responders during a meta election is the canonical one: deleting here
	// would turn a retryable blip into a fleet-wide delivery reset.
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
