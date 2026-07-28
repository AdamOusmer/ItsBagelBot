package k8s

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

// A hub member and a leaf share the node's network namespace, so their
// listeners are host-wide and every port either binds must be disjoint.
func TestHostNetworkListenersDoNotCollide(t *testing.T) {
	hub := sourceFile{name: "nats.yaml"}.read(t)
	leaf := sourceFile{name: "nats-leaf.yaml"}.read(t)

	requireHostNetns(t, "nats.yaml", hub)
	requireHostNetns(t, "nats-leaf.yaml", leaf)

	leafPorts := containerPorts(t, leaf)
	for port := range containerPorts(t, hub) {
		if leafPorts[port] {
			t.Errorf("hub and leaf both bind host port %s", port)
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

func requireHostNetns(t *testing.T, name, manifest string) {
	t.Helper()
	for _, required := range []string{"hostNetwork: true", "dnsPolicy: ClusterFirstWithHostNet"} {
		if !strings.Contains(manifest, required) {
			t.Fatalf("%s must set %q", name, required)
		}
	}
}

func containerPorts(t *testing.T, manifest string) map[string]bool {
	t.Helper()
	ports := make(map[string]bool)
	for _, match := range regexp.MustCompile(`containerPort: (\d+)`).FindAllStringSubmatch(manifest, -1) {
		ports[match[1]] = true
	}
	if len(ports) == 0 {
		t.Fatal("no container ports parsed; the manifest layout likely changed")
	}
	return ports
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
