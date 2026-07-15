package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
)

type capacityProfile struct {
	Replicas                    int      `json:"replicas"`
	RatedEPS                    int      `json:"rated_eps"`
	CeilingOfferedEPS           int      `json:"ceiling_offered_eps"`
	TargetUtilizationPct        int      `json:"target_utilization_pct"`
	OperatingEPS                int      `json:"operating_eps"`
	OperatingMinEPS             int      `json:"operating_min_eps"`
	Nodes                       []string `json:"nodes"`
	PublishersPerNode           int      `json:"publishers_per_node"`
	PublisherConnections        int      `json:"publisher_connections"`
	WindowPerPublisher          int      `json:"window_per_publisher"`
	PayloadBytes                int      `json:"payload_bytes"`
	PayloadVariants             int      `json:"payload_variants"`
	Storage                     string   `json:"storage"`
	MaxAge                      string   `json:"max_age"`
	MaxBytes                    int64    `json:"max_bytes"`
	MaxMsgsPerSubject           int64    `json:"max_msgs_per_subject"`
	DuplicateWindow             string   `json:"duplicate_window"`
	Dedup                       bool     `json:"dedup"`
	AllowAtomicPublish          bool     `json:"allow_atomic_publish"`
	AllowBatchPublish           bool     `json:"allow_batch_publish"`
	AtomicBatchSizes            []int    `json:"atomic_batch_sizes"`
	AtomicInflightPerConnection int      `json:"atomic_inflight_per_connection"`
	AtomicInflightFleet         int      `json:"atomic_inflight_fleet"`
	FastFlows                   []int    `json:"fast_flows"`
	FastOutstandingAcks         []int    `json:"fast_outstanding_acks"`
	CalibrationMessages         int      `json:"calibration_messages"`
	CalibrationDuration         string   `json:"calibration_duration"`
	LatencySamplesPerSecond     int      `json:"latency_samples_per_second"`
}

func TestR3CapacityProfileDefinesTheWantedOperatingEnvelope(t *testing.T) {
	profile := readCapacityProfile(t)
	if profile.Replicas != 3 || profile.RatedEPS != 120_000 || profile.CeilingOfferedEPS != 126_000 {
		t.Fatalf("unexpected R3 ceiling profile: %+v", profile)
	}
	if profile.TargetUtilizationPct != 75 || profile.OperatingEPS != 90_000 || profile.OperatingMinEPS != 89_100 {
		t.Fatalf("unexpected operating point: %+v", profile)
	}
	if profile.OperatingEPS != profile.RatedEPS*profile.TargetUtilizationPct/100 {
		t.Fatal("operating EPS is not exactly 75% of rated capacity")
	}
	if !slices.Equal(profile.Nodes, []string{"node2", "node3", "worker1"}) {
		t.Fatalf("nodes = %v", profile.Nodes)
	}
	if profile.PublishersPerNode != 2 || profile.PublisherConnections != 6 || profile.WindowPerPublisher != 16_384 {
		t.Fatalf("unexpected publisher shape: %+v", profile)
	}
	if profile.PayloadBytes != 256 || profile.PayloadVariants != 65_536 || profile.Storage != "memory" {
		t.Fatalf("unexpected payload/storage profile: %+v", profile)
	}
	if profile.MaxAge != "5m" || profile.MaxBytes != 1<<30 || profile.MaxMsgsPerSubject != 400_000 {
		t.Fatalf("unexpected retention profile: %+v", profile)
	}
	if profile.Dedup || profile.DuplicateWindow != "10s" ||
		!profile.AllowAtomicPublish || !profile.AllowBatchPublish {
		t.Fatalf("unexpected NATS 2.14 stream features: %+v", profile)
	}
	if profile.AtomicInflightFleet != profile.PublisherConnections*profile.AtomicInflightPerConnection {
		t.Fatal("fleet atomic in-flight count does not match connections x per-connection limit")
	}
	if profile.AtomicInflightFleet != 24 || profile.AtomicInflightFleet >= 50 {
		t.Fatalf("atomic in-flight fleet = %d", profile.AtomicInflightFleet)
	}
	if !slices.Equal(profile.AtomicBatchSizes, []int{32, 64, 128}) ||
		!slices.Equal(profile.FastFlows, []int{32, 64, 128}) ||
		!slices.Equal(profile.FastOutstandingAcks, []int{2, 4}) {
		t.Fatalf("unexpected calibration matrix: %+v", profile)
	}
	if profile.CalibrationMessages != 1_200_000 || profile.CalibrationDuration != "10s" ||
		profile.LatencySamplesPerSecond != 20 {
		t.Fatalf("unexpected calibration sampling profile: %+v", profile)
	}
}

func TestTemporaryR3StreamMatchesProductionShapedRetention(t *testing.T) {
	cfg := config{
		stream: "R3_SHADOW_TEST_ASYNC", subject: "twitch.outgress.bench.r3.test.async",
		replicas: 3, requiredPeers: 3,
	}
	stream := temporaryStreamConfig(cfg)
	if stream.Replicas != 3 || stream.Storage != jsapi.MemoryStorage || stream.Placement != nil {
		t.Fatalf("unexpected R3 placement/storage: %+v", stream)
	}
	if stream.MaxAge != 5*time.Minute || stream.MaxBytes != 1<<30 || stream.MaxMsgsPerSubject != 400_000 {
		t.Fatalf("unexpected R3 retention: %+v", stream)
	}
	if stream.Duplicates != 10*time.Second || !stream.AllowAtomicPublish || !stream.AllowBatchPublish {
		t.Fatalf("unexpected NATS 2.14 features: %+v", stream)
	}
}

func TestBenchmarkPublishingIsStructurallyDedupFree(t *testing.T) {
	profile := readCapacityProfile(t)
	if profile.Dedup {
		t.Fatal("R3 profile enabled deduplication")
	}
	msg := benchmarkMessage("twitch.outgress.bench.r3.test.async", []byte("payload"))
	if msg.Header.Get(nats.MsgIdHdr) != "" {
		t.Fatal("benchmark message carries Nats-Msg-Id")
	}
	if profile.DuplicateWindow != "10s" {
		t.Fatalf("duplicate window = %q", profile.DuplicateWindow)
	}
}

func TestDestructiveOperationsRejectProductionTargets(t *testing.T) {
	for _, target := range []struct{ stream, subject string }{
		{"TWITCH_INGRESS", "twitch.outgress.>"},
		{"TWITCH_OUTGRESS", "twitch.outgress.standard"},
		{"R3_SHADOW_TEST", "twitch.outgress.standard"},
		{"R3_SHADOW_TEST", "twitch.outgress.bench.r3.>"},
		{"R3_SHADOW_TEST", "twitch.outgress.bench.r3.*"},
		{"r3_shadow_test", "twitch.outgress.bench.r3.test"},
	} {
		if err := validateTemporaryTarget(target.stream, target.subject); err == nil {
			t.Fatalf("unsafe target passed: %+v", target)
		}
	}
	for _, target := range []struct{ stream, subject string }{
		{"R3_SHADOW_TEST_ASYNC", "twitch.outgress.bench.r3.test.async"},
		{"LIVE_NATS_ACCEPTANCE_TEST", "twitch.outgress.bench.test"},
		{"FLEET_700K_TEST", "twitch.outgress.bench.fleettest"},
	} {
		if err := validateTemporaryTarget(target.stream, target.subject); err != nil {
			t.Fatalf("safe target rejected: %v", err)
		}
	}
}

func TestR3ShadowCannotBePlacementPinnedOrDowngraded(t *testing.T) {
	base := config{
		stream: "R3_SHADOW_TEST", subject: "twitch.outgress.bench.r3.test",
		replicas: 3, requiredPeers: 3,
	}
	if err := validateR3ShadowConfig(base); err != nil {
		t.Fatal(err)
	}
	downgraded := base
	downgraded.replicas = 1
	if err := validateR3ShadowConfig(downgraded); err == nil {
		t.Fatal("R3 stream accepted one replica")
	}
	pinned := base
	pinned.placementTag = "nats-0"
	if err := validateR3ShadowConfig(pinned); err == nil {
		t.Fatal("R3 stream accepted a single-server placement tag")
	}
}

func TestTopologyRequiresWorkerFollowerAndZeroLag(t *testing.T) {
	info := healthyTopology()
	if !topologyReady(info, 3, "nats-2") {
		t.Fatal("healthy R3 topology did not pass")
	}
	info.Cluster.Leader = "nats-2"
	if topologyReady(info, 3, "nats-2") {
		t.Fatal("worker1 server passed as leader")
	}
	info = healthyTopology()
	info.Cluster.Replicas[1].Lag = 1
	if topologyReady(info, 3, "nats-2") {
		t.Fatal("lagging worker1 follower passed")
	}
}

func TestTopologyObserverDetectsLeaderChange(t *testing.T) {
	observer := topologyObserver{report: topologyReport{ForbiddenLeader: "nats-2"}}
	if err := observer.observe(healthyTopology()); err != nil {
		t.Fatal(err)
	}
	changed := healthyTopology()
	changed.Cluster.Leader = "nats-1"
	changed.Cluster.Replicas[0].Name = "nats-0"
	if err := observer.observe(changed); err != nil {
		t.Fatal(err)
	}
	if observer.report.LeaderChanges != 1 {
		t.Fatalf("leader changes = %d", observer.report.LeaderChanges)
	}
}

func TestLeaderAdvisoryDetectsTransientForbiddenLeader(t *testing.T) {
	watch := &leaderAdvisoryWatch{stream: "R3_SHADOW_TEST"}
	watch.observe(&nats.Msg{Data: []byte(`{"stream":"R3_SHADOW_TEST","leader":"nats-2"}`)})
	result := watch.snapshot(nil)
	report := topologyReport{ForbiddenLeader: "nats-2"}
	applyLeaderAdvisoryResult(&report, result)
	if !report.ForbiddenLeaderSeen || report.LeaderElectionAdvisories != 1 {
		t.Fatalf("transient leader advisory was not captured: %+v", report)
	}
	if err := validateTopologyReport(report); err == nil {
		t.Fatal("forbidden transient leader advisory passed topology validation")
	}
}

func TestR3RunnerIsGuardedAndNotKustomized(t *testing.T) {
	kustomization, err := os.ReadFile("../kustomization.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(kustomization, []byte("r3-120k")) {
		t.Fatal("R3 shadow runner was added to the production kustomization")
	}
	cmd := exec.Command("bash", "r3-120k.sh")
	cmd.Env = withoutEnv(os.Environ(), "CONFIRM_R3_SHADOW")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("guarded runner failed: %v: %s", err, output)
	}
	if !strings.Contains(string(output), "no actions performed") {
		t.Fatalf("guarded runner output = %q", output)
	}
	script, err := os.ReadFile("r3-120k.sh")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(script, []byte(`"$results_dir"/"$label"-*.json`)) {
		t.Fatal("R3 summary wildcard would include the topology report as a publisher result")
	}
	if !bytes.Contains(script, []byte(`"${result_files[@]}"`)) {
		t.Fatal("R3 summary does not use an explicit publisher result list")
	}
}

func TestLatencySampleRequirementUsesRateControlledDuration(t *testing.T) {
	cfg := config{
		messages: 42_000, targetRate: 42_000, latencySamples: 20,
		latencyInterval: 50 * time.Millisecond, maxP99: 50 * time.Millisecond,
	}
	if got := latencySampleRequirement(cfg); got != 8 {
		t.Fatalf("latency requirement = %d, want 8", got)
	}
	cfg.targetRate = 0
	if got := latencySampleRequirement(cfg); got != 1 {
		t.Fatalf("unpaced latency requirement = %d, want 1", got)
	}
	cfg.latencySamples = 0
	if got := latencySampleRequirement(cfg); got != 0 {
		t.Fatalf("disabled latency requirement = %d, want 0", got)
	}
}

func TestMissedPublisherStartBarrierFails(t *testing.T) {
	if _, err := waitForStart(time.Now().Add(-2 * time.Second).UnixMilli()); err == nil {
		t.Fatal("publisher accepted a missed shared start barrier")
	}
	started, err := waitForStart(0)
	if err != nil || started.IsZero() {
		t.Fatalf("unbarriered local start failed: started=%v err=%v", started, err)
	}
}

func TestPublishModesHonorCanceledProcessContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, mode := range []string{"async", "atomic", "fast"} {
		job := publishJob{
			ctx:      ctx,
			cfg:      config{mode: mode},
			counters: &benchmarkCounters{},
		}
		if err := publishByMode(job); !errors.Is(err, context.Canceled) {
			t.Fatalf("mode %s cancellation error = %v", mode, err)
		}
	}
}

func TestVariedPayloadRingIsFixedSizeValidJSON(t *testing.T) {
	payloads := benchmarkPayloads(256, 8)
	for i, payload := range payloads {
		if len(payload) != 256 || !json.Valid(payload) {
			t.Fatalf("payload %d length=%d valid=%t", i, len(payload), json.Valid(payload))
		}
	}
	if bytes.Equal(payloads[0], payloads[1]) {
		t.Fatal("payload variants are identical")
	}
}

func readCapacityProfile(t *testing.T) capacityProfile {
	t.Helper()
	data, err := os.ReadFile("r3-capacity.json")
	if err != nil {
		t.Fatal(err)
	}
	var profile capacityProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		t.Fatal(err)
	}
	return profile
}

func healthyTopology() *jsapi.StreamInfo {
	return &jsapi.StreamInfo{Cluster: &jsapi.ClusterInfo{
		Leader: "nats-0",
		Replicas: []*jsapi.PeerInfo{
			{Name: "nats-1", Current: true},
			{Name: "nats-2", Current: true},
		},
	}}
}

func withoutEnv(environment []string, key string) []string {
	prefix := key + "="
	filtered := make([]string, 0, len(environment))
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
