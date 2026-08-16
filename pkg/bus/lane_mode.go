// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strings"

	"ItsBagelBot/pkg/env"
)

// isHotIngressLane limits receipt-level acknowledgement flow control to the
// perishable high-rate event lanes. Stream/status control messages retain the
// ordinary explicit-ACK contract because losing one while an application
// process exits has a much larger blast radius than replaying a chat event.
// Work-queue streams are excluded by the same rule: their retention deletes on
// ack and therefore requires per-message explicit acknowledgement.
//
// Two streams qualify, not one, because the lanes are partitioned across two
// RAFT leaders: premium stays on TWITCH_INGRESS and standard lives on
// TWITCH_INGRESS_STANDARD. The subject test still decides which lane it is —
// the stream test only decides that the subject is an ingress lane at all — so
// a lane keeps its acknowledgement contract across the partition. Omitting the
// second stream here is silent: the standard lane would simply fall back to
// explicit acks and the receipt-level path would be premium-only.
func isHotIngressLane(stream, subject string) bool {
	if stream != TwitchIngressStream.Name && stream != TwitchIngressStandardStream.Name {
		return false
	}
	return strings.HasSuffix(subject, ".premium") || strings.HasSuffix(subject, ".standard")
}

// FlowConsumeEnabled reports whether the hot ingress lanes bind receipt-level
// flow-controlled consumers. NATS_CONSUME_FLOW=on selects them; anything else,
// including unset, keeps the explicit-ACK subscriber.
//
// It ships off, and the default is the whole contract: this flag is set in no
// manifest under deploy/, so every consumer takes it the moment its image rolls.
// Turning it on changes three things at once for the hot lanes — the server stops
// tracking per-message pending state and advances one replicated ack floor per
// window, a handler error can no longer NAK and is instead scheduled onto
// TWITCH_INGRESS_RETRY, and that stream has to exist and be drained. Enabling it
// therefore means provisioning the retry lane in the owning service AND
// subscribing it in the same change. A default of "on" would do the first two
// and not the third: failures would be scheduled onto a lane nobody reads and
// silently expire.
func FlowConsumeEnabled() bool {
	return env.Get("NATS_CONSUME_FLOW", "off") == "on"
}

// laneConsumeMode is the acknowledgement contract the hot ingress lanes bind
// under. The three are genuinely different shapes, not settings of one shape:
//
//	flow      per-pod AckFlowControl push consumer. Every pod receives the WHOLE
//	          lane, so a pod added by the autoscaler multiplies delivery and
//	          handler work rather than dividing it.
//	pull      one fleet-wide durable, fetched by every pod. The server hands each
//	          message to exactly one pod, so a pod added divides the lane.
//	explicit  the ordinary durable queue-group consumer with per-message acks.
type laneConsumeMode string

const (
	laneModeFlow     laneConsumeMode = "flow"
	laneModePull     laneConsumeMode = "pull"
	laneModeExplicit laneConsumeMode = "explicit"
)

// consumeMode resolves the configured contract. The default is deliberately
// unchanged: flow is what production runs, and this knob exists to A/B pull
// against it rather than to switch the fleet over.
//
// NATS_CONSUME_FLOW=off predates NATS_CONSUME_MODE and still wins outright,
// because it is set in deployed manifests as the kill switch back to explicit
// acks — an operator reaching for it during an incident must not have to know
// that a second variable exists. An unrecognised mode falls back to pull for
// the same reason: a typo must not silently change the lane's shape.
//
// Pull is the default receipt-level shape. The flow consumer fans out per
// consumer name, so every pod added by the autoscaler receives a full copy of
// the lane and multiplies the broker's delivery egress; the shared-durable pull
// consumer divides the lane instead (live-measured 2026-08-15: two pull
// instances per stream drained a 180k/s two-stream load the fan-out shape
// could not, at 1x delivery egress). Ordering stays intact where it matters:
// multi-line responses ship as one TypeBatch event, so cross-pod interleave
// only touches independent events. Flow remains selectable per deployment for
// any lane later shown to need a per-pod cursor.
func consumeMode() laneConsumeMode {
	if !FlowConsumeEnabled() {
		return laneModeExplicit
	}
	switch laneConsumeMode(env.Get("NATS_CONSUME_MODE", string(laneModePull))) {
	case laneModeFlow:
		return laneModeFlow
	case laneModeExplicit:
		return laneModeExplicit
	default:
		return laneModePull
	}
}
