// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// sourceFile reads a file relative to this package's directory. NATS moved to
// deploy/messaging (see nats.yaml's reference below), so this is a second,
// package-local copy of the same tiny helper deploy/messaging/nats_auth_test.go
// defines for its own package — not shared, because the two packages read from
// two different directories and Go has no cross-package file-relative helper.
type sourceFile struct {
	name string
}

func (f sourceFile) read(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(f.name)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

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
			manifest := sourceFile{name: publisher.manifest}.read(t)
			// Either spelling of the hub Service satisfies the contract: the
			// point is that the publisher dials the hub Service rather than a
			// leaf or a pod, and `nats.messaging` and its
			// `.svc.cluster.local` FQDN are the same Service. Pinning only the
			// short form failed the manifests that (correctly) spell it out to
			// skip the search-domain walk.
			value := strings.Replace(regexp.QuoteMeta(publisher.value),
				`nats\.messaging`, `nats\.messaging(?:\.svc\.cluster\.local)?`, 1)
			pattern := regexp.MustCompile(`(?m)^\s*- name: ` + regexp.QuoteMeta(publisher.variable) +
				`\n\s+value: ` + value + `$`)
			if !pattern.MatchString(manifest) {
				t.Fatalf("%s must set %s=%s", publisher.manifest, publisher.variable, publisher.value)
			}
		})
	}
}

func TestHubServicePrefersSameNode(t *testing.T) {
	// nats.yaml lives in deploy/messaging now, not this directory.
	manifest := sourceFile{name: "../messaging/nats.yaml"}.read(t)
	service := regexp.MustCompile(`(?s)kind: Service\nmetadata:.*?\n  name: nats\n.*?trafficDistribution: PreferSameNode`).FindString(manifest)
	if service == "" {
		t.Fatal("nats Service must retain trafficDistribution: PreferSameNode")
	}
}
