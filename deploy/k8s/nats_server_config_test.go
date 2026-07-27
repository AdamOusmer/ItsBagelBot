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

// TestHubCarriesNoNonReloadableJetStreamLimits is the guard for the whole
// SIGHUP delivery model. jetstream.limits (batch inflight, duplicate window,
// max HA assets, …) lands in Options.JetStreamLimits, which reload.go has no
// diff case for: it falls into the "config reload not supported" arm and aborts
// the entire reload. This configmap is delivered by SIGHUP on content change
// (kustomization.yaml keeps the name stable on purpose), so one such key does
// not merely fail to apply — it makes every later reload fail too, taking the
// ACL hot-push and the cert-manager TLS renewal with it until an operator
// restarts all three members by hand. If a limit is ever genuinely needed it
// ships with a forced pod roll, and this test changes with it.
func TestHubCarriesNoNonReloadableJetStreamLimits(t *testing.T) {
	config := sourceFile{name: "nats-server.conf"}.read(t)
	uncommented := regexp.MustCompile(`(?m)^\s*#.*$`).ReplaceAllString(config, "")

	if regexp.MustCompile(`(?m)^\s*limits\s*\{`).MatchString(uncommented) {
		t.Fatal("jetstream.limits is not hot-reloadable; a SIGHUP-delivered config must not set it")
	}
	if strings.Contains(uncommented, "max_inflight_per_stream") ||
		strings.Contains(uncommented, "max_inflight_total") {
		t.Fatal("batch inflight limits are not hot-reloadable and the shipped concurrency (24/stream) is under the defaults")
	}
}

// TestHubMemoryCeilingStaysReloadable pins max_mem where it is. The server
// refuses to hot-reload a *decrease* of jetstream max memory or store, so
// lowering either number here wedges every subsequent SIGHUP; both are raise-
// only edits, and a lower ceiling is a deliberate pod roll.
func TestHubMemoryCeilingStaysReloadable(t *testing.T) {
	config := sourceFile{name: "nats-server.conf"}.read(t)
	for _, required := range []string{"max_mem: 4GB", "max_file: 2GB"} {
		if !strings.Contains(config, required) {
			t.Fatalf("JetStream ceiling %q changed; a decrease cannot be delivered by SIGHUP", required)
		}
	}
}

// TestHubReadinessGatesOnAssetCurrency keeps a rolling restart from draining
// the quorum. js-enabled-only short-circuits before the stream/consumer walk,
// so a member that held nothing yet reported Ready in seconds and the roll moved
// on — restart all three that way and the memory-backed ingress window is gone.
// The full walk plus minReadySeconds is what makes the roll wait for a real
// catch-up.
func TestHubReadinessGatesOnAssetCurrency(t *testing.T) {
	manifest := sourceFile{name: "nats.yaml"}.read(t)

	probe := regexp.MustCompile(`readinessProbe:\s*\n\s*httpGet: \{path: ([^,]+),`).FindStringSubmatch(manifest)
	if probe == nil {
		t.Fatal("nats.yaml has no readinessProbe httpGet path")
	}
	if probe[1] != "/healthz" {
		t.Fatalf("readiness path = %q, want the full /healthz: any js-enabled-only form short-circuits before the asset walk, so a roll can drain all three replicas",
			probe[1])
	}
	if !regexp.MustCompile(`(?m)^\s*minReadySeconds: \d+`).MatchString(manifest) {
		t.Fatal("no minReadySeconds; the roll proceeds the instant a member first reports Ready")
	}
	if !strings.Contains(manifest, "kind: PodDisruptionBudget") ||
		!regexp.MustCompile(`(?m)^\s*maxUnavailable: 1`).MatchString(manifest) {
		t.Fatal("no maxUnavailable:1 budget; a node drain can evict two of three voters at once")
	}
}
