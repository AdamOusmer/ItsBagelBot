// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

func TestOnlyHotIngressLanesUseFlowControl(t *testing.T) {
	for _, test := range []struct {
		stream  string
		subject string
		want    bool
	}{
		{TwitchIngressStream.Name, "twitch.ingress.event.premium", true},
		{TwitchIngressStandardStream.Name, "twitch.ingress.event.standard", true},
		{TwitchIngressStream.Name, "twitch.ingress.event.stream", false},
		{TwitchIngressStream.Name, "twitch.ingress.status.authz.revoked", false},
		{TwitchIngressRetryStream.Name, "twitch.ingress.retry.standard", false},
		{OutgressStream.Name, "twitch.outgress.standard", false},
		{BagelDataStream.Name, "data.users.updated", false},
	} {
		if got := isHotIngressLane(test.stream, test.subject); got != test.want {
			t.Fatalf("isHotIngressLane(%q, %q) = %v, want %v", test.stream, test.subject, got, test.want)
		}
	}
}

func TestFlowConsumerConfigIsAcceptedByTheServerContract(t *testing.T) {
	cfg := flowConsumerConfig(laneBinding{subject: "twitch.ingress.event.premium", consumer: "worker_premium_pod_1"})

	requireContract(t,
		contractClause{cfg.AckPolicy == jsapi.AckFlowControlPolicy,
			fmt.Sprintf("ack policy = %v, want AckFlowControl", cfg.AckPolicy)},
		contractClause{cfg.FlowControl, "flow contract violated: flow control off"},
		contractClause{cfg.DeliverSubject != "", "flow contract violated: not a push consumer"},
		contractClause{cfg.IdleHeartbeat == time.Second,
			fmt.Sprintf("idle heartbeat = %v, want the mandated second", cfg.IdleHeartbeat)},
		contractClause{cfg.MaxAckPending == flowMaxAckPending,
			fmt.Sprintf("max ack pending = %d, want %d", cfg.MaxAckPending, flowMaxAckPending)},
		contractClause{cfg.Replicas == 1 && cfg.MemoryStorage,
			fmt.Sprintf("consumer state must be R1 in memory: %#v", cfg)},
		contractClause{cfg.DeliverPolicy == jsapi.DeliverNewPolicy,
			fmt.Sprintf("deliver policy = %v, want DeliverNew", cfg.DeliverPolicy)},
	)
}

func TestFlowConsumerIsSingleSubscriberPerPod(t *testing.T) {
	t.Setenv("POD_NAME", "sesame-6d9f7c8b45-tq2xz")
	name := flowConsumerName("worker", "twitch.ingress.event.premium")
	cfg := flowConsumerConfig(laneBinding{subject: "twitch.ingress.event.premium", consumer: name})

	if cfg.DeliverGroup != "" {
		t.Fatalf("flow consumer bound a queue group: %q", cfg.DeliverGroup)
	}
	if !strings.HasSuffix(name, "sesame-6d9f7c8b45-tq2xz") {
		t.Fatalf("consumer name = %q, want this pod's identity", name)
	}
	if cfg.DeliverSubject != "_INBOX.BAGEL."+subjectToken(name) {
		t.Fatalf("delivery subject = %q, want one derived from the pod's consumer", cfg.DeliverSubject)
	}
	if cfg.InactiveThreshold != flowInactiveThreshold {
		t.Fatalf("inactive threshold = %v, want %v", cfg.InactiveThreshold, flowInactiveThreshold)
	}
}

func TestPodIdentityFallsBackAndStaysNameSafe(t *testing.T) {
	t.Setenv("POD_NAME", "")
	t.Setenv("HOSTNAME", "sesame.node-3.local")
	if got := podIdentity(); got != "sesame_node-3_local" {
		t.Fatalf("pod identity = %q, want the dots replaced", got)
	}
	if got := consumerToken(strings.Repeat("x", 200)); len(got) != 48 {
		t.Fatalf("token length = %d, want the durable bounded at 48", len(got))
	}
	if got := consumerToken("pod*name>with.wildcards"); strings.ContainsAny(got, ".*>") {
		t.Fatalf("token %q still carries subject wildcards", got)
	}
}

func TestFlowConsumerRecoveryResumesAfterTheReceiptCursor(t *testing.T) {
	cfg := recoveryFlowConsumerConfig(
		laneBinding{subject: "twitch.ingress.event.standard", consumer: "worker_standard_pod_1"},
		flowPosition{consumer: 90, stream: 120},
	)
	if cfg.DeliverPolicy != jsapi.DeliverByStartSequencePolicy || cfg.OptStartSeq != 121 {
		t.Fatalf("recovery policy=%v start=%d, want start sequence 121", cfg.DeliverPolicy, cfg.OptStartSeq)
	}

	fresh := recoveryFlowConsumerConfig(laneBinding{subject: "twitch.ingress.event.standard", consumer: "worker_standard_pod_1"}, flowPosition{})
	if fresh.DeliverPolicy != jsapi.DeliverNewPolicy {
		t.Fatalf("empty cursor recovery policy = %v, want DeliverNew", fresh.DeliverPolicy)
	}
}

func TestCarriedAckFloorSurvivesConsumerReplacement(t *testing.T) {
	desired := flowConsumerConfig(laneBinding{subject: "twitch.ingress.event.premium", consumer: "worker_premium_pod_1"})
	info := &jsapi.ConsumerInfo{}
	info.AckFloor.Stream = 4_200

	carryLaneAckFloor(&desired, info)
	if desired.DeliverPolicy != jsapi.DeliverByStartSequencePolicy || desired.OptStartSeq != 4_201 {
		t.Fatalf("replacement resumed at %v/%d", desired.DeliverPolicy, desired.OptStartSeq)
	}
}

func TestUnknownAckFloorNeverInheritsThePredecessorPolicy(t *testing.T) {
	desired := flowConsumerConfig(laneBinding{subject: "twitch.ingress.event.premium", consumer: "worker_premium_pod_1"})
	desired.DeliverPolicy = jsapi.DeliverAllPolicy
	desired.OptStartSeq = 77

	carryLaneAckFloor(&desired, &jsapi.ConsumerInfo{})
	if desired.DeliverPolicy != jsapi.DeliverNewPolicy || desired.OptStartSeq != 0 {
		t.Fatalf("zero ack floor resumed at %v/%d, want DeliverNew", desired.DeliverPolicy, desired.OptStartSeq)
	}
}

func TestOnlyImmutableFieldErrorsReplaceTheConsumer(t *testing.T) {
	for _, test := range []struct {
		err  error
		want bool
	}{
		{errors.New("nats: ack policy can not be updated"), true},
		{errors.New("nats: flow control can not be updated"), true},
		{errors.New("nats: heart beats can not be updated"), true},
		{errors.New("nats: can not update push consumer to pull based"), true},
		{errors.New("nats: can not update pull consumer to push based"), true},
		{context.DeadlineExceeded, false},
		{nats.ErrNoResponders, false},
		{jsapi.ErrConsumerNotFound, false},
		{errors.New("nats: max waiting can not be updated"), false},
		{errors.New("nats: consumer name already in use"), false},
		{nil, false},
	} {
		if got := requiresConsumerReplacement(test.err); got != test.want {
			t.Fatalf("requiresConsumerReplacement(%v) = %v, want %v", test.err, got, test.want)
		}
	}
}

func TestFlowConsumptionIsOptIn(t *testing.T) {
	for _, value := range []string{"", "off", "yes", "true", "1", "ON"} {
		t.Setenv("NATS_CONSUME_FLOW", value)
		if FlowConsumeEnabled() {
			t.Errorf("NATS_CONSUME_FLOW=%q enabled flow consumption; only the literal \"on\" may", value)
		}
	}
	t.Setenv("NATS_CONSUME_FLOW", "on")
	if !FlowConsumeEnabled() {
		t.Fatal("NATS_CONSUME_FLOW=on did not select receipt-level flow control")
	}
}

func TestFlowConsumerScopeGuardDeclinesControlLanes(t *testing.T) {
	t.Setenv("NATS_CONSUME_FLOW", "on")
	t.Setenv("NATS_CONSUME_MODE", "flow")
	subscriber := &fleetSubscriber{group: "worker"}
	hot := subscriptionTarget{stream: TwitchIngressStream.Name, topic: "twitch.ingress.event.premium"}
	control := subscriptionTarget{stream: TwitchIngressStream.Name, topic: "twitch.ingress.status.authz.revoked"}

	if subscriber.laneModeFor(hot) != laneModeFlow {
		t.Fatal("the hot premium lane was refused a flow consumer")
	}
	if subscriber.laneModeFor(control) != laneModeExplicit {
		t.Fatal("a status lane was given a flow consumer")
	}

	t.Setenv("NATS_CONSUME_FLOW", "off")
	if subscriber.laneModeFor(hot) != laneModeExplicit {
		t.Fatal("NATS_CONSUME_FLOW=off still bound a flow consumer")
	}
}

func TestPartitionedLanesBindReceiptLevelConsumers(t *testing.T) {
	t.Setenv("NATS_INGRESS_PARTITION", "on")
	for _, mode := range []struct {
		configured string
		want       laneConsumeMode
	}{
		{"pull", laneModePull},
		{"flow", laneModeFlow},
	} {
		t.Run(mode.configured, func(t *testing.T) {
			t.Setenv("NATS_CONSUME_FLOW", "on")
			t.Setenv("NATS_CONSUME_MODE", mode.configured)
			subscriber := &fleetSubscriber{group: "worker"}

			for _, want := range []laneExpectation{
				{subject: "twitch.ingress.event.standard", stream: TwitchIngressStandardStream.Name, mode: mode.want},
				{subject: "twitch.ingress.event.premium", stream: TwitchIngressStream.Name, mode: mode.want},
			} {
				requireLaneBinding(t, subscriber, want)
			}
		})
	}
}

type laneExpectation struct {
	subject string
	stream  string
	mode    laneConsumeMode
}

func requireLaneBinding(t *testing.T, subscriber *fleetSubscriber, want laneExpectation) {
	t.Helper()
	target, err := targetForTopic(want.subject)
	if err != nil {
		t.Fatalf("targetForTopic(%q): %v", want.subject, err)
	}
	if target.stream != want.stream {
		t.Fatalf("%q binds stream %q, want %q", want.subject, target.stream, want.stream)
	}
	if got := subscriber.laneModeFor(target); got != want.mode {
		t.Fatalf("%q on %q got mode %q, want %q", want.subject, target.stream, got, want.mode)
	}
}
