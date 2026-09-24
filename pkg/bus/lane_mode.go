// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"strings"

	"ItsBagelBot/pkg/env"
)

func isHotIngressLane(stream, subject string) bool {
	if stream != TwitchIngressStream.Name && stream != TwitchIngressStandardStream.Name {
		return false
	}
	return strings.HasSuffix(subject, ".premium") || strings.HasSuffix(subject, ".standard")
}

func IsCanaryLane(stream, subject string) bool {
	if env.Get("NATS_CONSUME_CANARY", "off") != "on" {
		return false
	}
	return stream == TwitchIngressRetryStream.Name &&
		strings.HasPrefix(subject, "twitch.ingress.retry.canary.")
}

// Enable only with TWITCH_INGRESS_RETRY provisioned and subscribed, or failed events expire.
func FlowConsumeEnabled() bool {
	return env.Get("NATS_CONSUME_FLOW", "off") == "on"
}

type laneConsumeMode string

const (
	laneModeFlow     laneConsumeMode = "flow"
	laneModePull     laneConsumeMode = "pull"
	laneModeExplicit laneConsumeMode = "explicit"
)

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
