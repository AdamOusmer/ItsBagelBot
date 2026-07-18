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
