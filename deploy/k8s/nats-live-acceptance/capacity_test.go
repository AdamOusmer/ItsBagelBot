package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
)

type capacityProfile struct {
	Stream                      string   `json:"stream"`
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
	CalibrationTargetEPS        int      `json:"calibration_target_eps"`
	CalibrationDuration         string   `json:"calibration_duration"`
	LatencySamplesPerSecond     int      `json:"latency_samples_per_second"`
	PubAckP99Max                string   `json:"puback_p99_max"`
	RPCP99Max                   string   `json:"rpc_p99_max"`
	RPCP99MinSamples            int      `json:"rpc_p99_min_samples"`
	MaxAckGap                   string   `json:"max_ack_gap"`
}

func TestR3CapacityProfileDefinesRateEnvelope(t *testing.T) {
	profile := readCapacityProfile(t)
	require.Equal(t, "R3_SHADOW_BENCH", profile.Stream)
	require.Equal(t, 3, profile.Replicas)
	require.Equal(t, 120_000, profile.RatedEPS)
	require.Equal(t, 126_000, profile.CeilingOfferedEPS)
	require.Equal(t, 75, profile.TargetUtilizationPct)
	require.Equal(t, 90_000, profile.OperatingEPS)
	require.Equal(t, 89_100, profile.OperatingMinEPS)
	require.Equal(t, profile.RatedEPS*profile.TargetUtilizationPct/100, profile.OperatingEPS)
}

func TestR3CapacityProfileDefinesPublisherShape(t *testing.T) {
	profile := readCapacityProfile(t)
	require.Equal(t, []string{"node2", "node3", "worker1"}, profile.Nodes)
	require.Equal(t, 2, profile.PublishersPerNode)
	require.Equal(t, 6, profile.PublisherConnections)
	require.Equal(t, 16_384, profile.WindowPerPublisher)
	require.Equal(t, 256, profile.PayloadBytes)
	require.Equal(t, 65_536, profile.PayloadVariants)
	require.Equal(t, "memory", profile.Storage)
}

func TestR3CapacityProfileDefinesRetention(t *testing.T) {
	profile := readCapacityProfile(t)
	require.Equal(t, "5m", profile.MaxAge)
	require.Equal(t, int64(1<<30), profile.MaxBytes)
	require.Equal(t, int64(400_000), profile.MaxMsgsPerSubject)
}

func TestR3CapacityProfileDefinesNATS214Features(t *testing.T) {
	profile := readCapacityProfile(t)
	require.False(t, profile.Dedup)
	require.Equal(t, "10s", profile.DuplicateWindow)
	require.True(t, profile.AllowAtomicPublish)
	require.True(t, profile.AllowBatchPublish)
}

func TestR3CapacityProfileDefinesCalibrationMatrix(t *testing.T) {
	profile := readCapacityProfile(t)
	require.Equal(
		t,
		profile.PublisherConnections*profile.AtomicInflightPerConnection,
		profile.AtomicInflightFleet,
	)
	require.Equal(t, 24, profile.AtomicInflightFleet)
	require.Less(t, profile.AtomicInflightFleet, 50)
	require.Equal(t, []int{32, 64, 128}, profile.AtomicBatchSizes)
	require.Equal(t, []int{32, 64, 128}, profile.FastFlows)
	require.Equal(t, []int{2, 4}, profile.FastOutstandingAcks)
	require.Equal(t, 1_200_000, profile.CalibrationMessages)
	require.Equal(t, 120_000, profile.CalibrationTargetEPS)
	require.Equal(t, "10s", profile.CalibrationDuration)
	require.Equal(t, 20, profile.LatencySamplesPerSecond)
	require.Equal(t, "2ms", profile.PubAckP99Max)
	require.Equal(t, "8ms", profile.RPCP99Max)
	require.Equal(t, 330, profile.RPCP99MinSamples)
	require.Equal(t, "2s", profile.MaxAckGap)
}

func TestTemporaryR3StreamMatchesProductionShapedRetention(t *testing.T) {
	cfg := config{
		stream: "R3_SHADOW_TEST_ASYNC", subject: "twitch.outgress.bench.r3.test.async",
		replicas: 3, requiredPeers: 3, maxMsgsPerSubject: 400_000,
	}
	stream := temporaryStreamConfig(cfg)
	require.Equal(t, 3, stream.Replicas)
	require.Equal(t, jsapi.MemoryStorage, stream.Storage)
	require.Nil(t, stream.Placement)
	require.Equal(t, 5*time.Minute, stream.MaxAge)
	require.Equal(t, int64(1<<30), stream.MaxBytes)
	require.Equal(t, int64(400_000), stream.MaxMsgsPerSubject)
	require.Equal(t, 10*time.Second, stream.Duplicates)
	require.True(t, stream.AllowAtomicPublish)
	require.True(t, stream.AllowBatchPublish)
}

func TestTemporaryR3StreamCanUseByteOnlyRollingRetention(t *testing.T) {
	stream := temporaryStreamConfig(config{
		stream: "R3_SHADOW_TEST_UNLIMITED", subject: "twitch.outgress.bench.r3.test.unlimited",
		replicas: 3, requiredPeers: 3, maxMsgsPerSubject: -1,
	})
	require.Equal(t, int64(-1), stream.MaxMsgsPerSubject)
	require.Equal(t, int64(1<<30), stream.MaxBytes)
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

func TestCleanupRequiresExactStreamAndSubjectOwnership(t *testing.T) {
	cfg := config{stream: "R3_SHADOW_BENCH", subject: "twitch.outgress.bench.r3.run.async"}
	owned := &jsapi.StreamInfo{Config: jsapi.StreamConfig{
		Name: cfg.stream, Subjects: []string{cfg.subject},
	}}
	require.NoError(t, validateCleanupOwnership(cfg, owned))

	wrongSubject := *owned
	wrongSubject.Config.Subjects = []string{"twitch.outgress.bench.r3.someone-else"}
	require.Error(t, validateCleanupOwnership(cfg, &wrongSubject))

	wrongName := *owned
	wrongName.Config.Name = "R3_SHADOW_OTHER"
	require.Error(t, validateCleanupOwnership(cfg, &wrongName))
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
