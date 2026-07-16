package k8s

import (
	"regexp"
	"testing"
)

func TestJetStreamPublishersUseNodeLocalHubService(t *testing.T) {
	publishers := []struct {
		manifest string
		variable string
		value    string
	}{
		{"twitch-ingress.yaml", "NATS_HUB_HOST", "nats"},
		{"commands.yaml", "NATS_HUB_PUBLISH_URL", "nats://nats:4222"},
		{"modules.yaml", "NATS_HUB_PUBLISH_URL", "nats://nats:4222"},
		{"projector.yaml", "NATS_HUB_PUBLISH_URL", "nats://nats:4222"},
		{"sesame.yaml", "NATS_HUB_PUBLISH_URL", "tls://nats:4222"},
		{"users.yaml", "NATS_HUB_PUBLISH_URL", "nats://nats:4222"},
	}

	for _, publisher := range publishers {
		t.Run(publisher.manifest, func(t *testing.T) {
			manifest := sourceFile{name: publisher.manifest}.read(t)
			pattern := regexp.MustCompile(`(?m)^\s*- name: ` + regexp.QuoteMeta(publisher.variable) +
				`\n\s+value: ` + regexp.QuoteMeta(publisher.value) + `$`)
			if !pattern.MatchString(manifest) {
				t.Fatalf("%s must set %s=%s", publisher.manifest, publisher.variable, publisher.value)
			}
		})
	}
}

func TestHubServicePrefersSameNode(t *testing.T) {
	manifest := sourceFile{name: "nats.yaml"}.read(t)
	service := regexp.MustCompile(`(?s)kind: Service\nmetadata:.*?\n  name: nats\n.*?trafficDistribution: PreferSameNode`).FindString(manifest)
	if service == "" {
		t.Fatal("nats Service must retain trafficDistribution: PreferSameNode")
	}
}
