// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestJetStreamPublishersUseNodeLocalHubService(t *testing.T) {
	publishers := []struct {
		manifest string
		variable string
		value    string
	}{
		{"twitch-ingress.yaml", "NATS_HUB_HOST", "nats.messaging"},
		{"commands.yaml", "NATS_HUB_PUBLISH_URL", "tls://nats.messaging:4222"},
		{"modules.yaml", "NATS_HUB_PUBLISH_URL", "tls://nats.messaging:4222"},
		{"projector.yaml", "NATS_HUB_PUBLISH_URL", "tls://nats.messaging:4222"},
		{"sesame.yaml", "NATS_HUB_PUBLISH_URL", "tls://nats.messaging:4222"},
		{"users.yaml", "NATS_HUB_PUBLISH_URL", "tls://nats.messaging:4222"},
	}

	for _, publisher := range publishers {
		t.Run(publisher.manifest, func(t *testing.T) {
			value := strings.Replace(regexp.QuoteMeta(publisher.value),
				`nats\.messaging`, `nats\.messaging(?:\.svc\.cluster\.local)?`, 1)
			pattern := regexp.MustCompile(`(?m)^\s*- name: ` + regexp.QuoteMeta(publisher.variable) +
				`\n\s+value: ` + value + `$`)

			assert.Regexp(t, pattern, readText(t, publisher.manifest), "%s must set %s=%s", publisher.manifest, publisher.variable, publisher.value)
		})
	}
}

func TestHubServicePrefersSameNode(t *testing.T) {
	manifest := readText(t, "../messaging/nats.yaml")

	assert.Regexp(t, regexp.MustCompile(`(?s)kind: Service\nmetadata:.*?\n  name: nats\n.*?trafficDistribution: PreferSameNode`), manifest,
		"nats Service must retain trafficDistribution: PreferSameNode")
}
