// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"regexp"
	"strings"
	"testing"
)

func TestHubRoutesRetainMeasuredCompressionMode(t *testing.T) {
	config := sourceFile{name: "nats-server.conf"}.read(t)
	cluster := regexp.MustCompile(`(?s)cluster \{.*?\n\}`).FindString(config)
	if cluster == "" {
		t.Fatal("nats-server.conf has no cluster block")
	}
	if !regexp.MustCompile(`(?m)^\s*mode:\s*s2_fast\s*$`).MatchString(cluster) {
		t.Fatal("BUS routes must retain the measured s2_fast compression mode")
	}
	if regexp.MustCompile(`(?m)^\s*mode:\s*s2_auto\s*$`).MatchString(cluster) ||
		regexp.MustCompile(`(?m)^\s*rtt_thresholds:`).MatchString(cluster) {
		t.Fatal("adaptive route compression regressed the asymmetric R3 topology")
	}
}

// NATS runs on the Cilium pod network like every other workload (hostNetwork
// was removed 2026-08-18; it dated from the old fleet's WireGuard carve-out).
// Guard the regression: hostNetwork reappearing would silently change what the
// members advertise (node IPs instead of the headless FQDNs the certs cover).
func TestPodNetworkOnly(t *testing.T) {
	for _, name := range []string{"nats.yaml", "nats-leaf.yaml"} {
		if strings.Contains(sourceFile{name: name}.read(t), "hostNetwork") {
			t.Errorf("%s must not set hostNetwork: members advertise headless FQDNs on the pod network", name)
		}
	}
}

// Moving the leaf listeners must stay invisible to callers: the routed
// Services keep publishing the ports every client URL already names.
func TestLeafServicesKeepPublishedClientPorts(t *testing.T) {
	manifest := sourceFile{name: "nats-leaf.yaml"}.read(t)
	published := map[string]int{
		"- {name: client, port: 4222, targetPort: client}":   2,
		"- {name: monitor, port: 8222, targetPort: monitor}": 2,
	}
	for entry, want := range published {
		if got := strings.Count(manifest, entry); got != want {
			t.Errorf("%q appears %d times, want %d (nats-leaf + nats-leaf-local)", entry, got, want)
		}
	}
}

func TestHubJetStreamIngestWindowStaysByteBounded(t *testing.T) {
	config := sourceFile{name: "nats-server.conf"}.read(t)
	jetstream := regexp.MustCompile(`(?s)jetstream \{.*?\n\}`).FindString(config)
	if jetstream == "" {
		t.Fatal("nats-server.conf has no JetStream block")
	}
	for _, required := range []string{
		"max_buffered_msgs: 262144",
		"max_buffered_size: 128MB",
	} {
		if !strings.Contains(jetstream, required) {
			t.Fatalf("JetStream ingest guard %q is missing", required)
		}
	}
}
