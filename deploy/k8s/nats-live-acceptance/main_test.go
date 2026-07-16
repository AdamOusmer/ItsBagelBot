package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
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

func TestPreferredLeaderMustMatchExactly(t *testing.T) {
	info := healthyTopology()
	require.True(t, preferredLeaderActive(info, "nats-0"))
	require.False(t, preferredLeaderActive(info, "nats-1"))
	require.True(t, preferredLeaderActive(info, ""))
	require.False(t, preferredLeaderActive(nil, "nats-0"))
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

func TestTopologyObserverRejectsTransientUnhealthyFollower(t *testing.T) {
	for _, mutate := range []func(*jsapi.PeerInfo){
		func(peer *jsapi.PeerInfo) { peer.Current = false },
		func(peer *jsapi.PeerInfo) { peer.Offline = true },
	} {
		info := healthyTopology()
		mutate(info.Cluster.Replicas[0])
		observer := topologyObserver{report: topologyReport{ForbiddenLeader: "nats-2"}}
		require.ErrorContains(t, observer.observe(info), "became unhealthy")
	}
}

func TestTopologyObserverAllowsBoundedFollowerCatchup(t *testing.T) {
	started := time.Now()
	observer := topologyObserver{
		report:         topologyReport{ForbiddenLeader: "nats-2"},
		unhealthyGrace: 5 * time.Second,
		unhealthySince: make(map[string]time.Time),
	}
	info := healthyTopology()
	info.Cluster.Replicas[0].Current = false
	require.NoError(t, observer.rejectUnhealthyFollowers(info.Cluster.Replicas, started))
	require.NoError(t, observer.rejectUnhealthyFollowers(info.Cluster.Replicas, started.Add(4*time.Second)))
	require.ErrorContains(
		t,
		observer.rejectUnhealthyFollowers(info.Cluster.Replicas, started.Add(5*time.Second)),
		"became unhealthy",
	)

	info.Cluster.Replicas[0].Current = true
	require.NoError(t, observer.rejectUnhealthyFollowers(info.Cluster.Replicas, started.Add(6*time.Second)))
	require.Empty(t, observer.unhealthySince)
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
	require.NoError(t, err)
	require.NotContains(t, string(kustomization), "r3-120k")

	cmd := exec.Command("bash", "r3-120k.sh")
	cmd.Env = withoutEnv(os.Environ(), "CONFIRM_R3_SHADOW")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "no actions performed")
}

func TestR3RunnerUsesExplicitResultsAndScopedSecrets(t *testing.T) {
	script := readR3Runner(t)
	require.NotContains(t, script, `"$results_dir"/"$label"-*.json`)
	require.Contains(t, script, `"${result_files[@]}"`)
	require.NotContains(t, script, `name:"worker-env"`)
	require.NotContains(t, script, `key:"VALKEY_ADDR"`)
	require.NotContains(t, script, `control_pod=${pods[`)
	assertScriptContains(t, script,
		`NATS_BENCH_PUBLISHER_SECRET:-sesame-env`,
		`NATS_BENCH_ADMIN_RPC_SECRET:-console-admin-env`,
		`if $role == "control" then`,
		`elif $role == "sli" then`,
		`valkey.valkey.svc.cluster.local:26380`,
		`VALKEY_LOCAL_ADDR`,
	)
}

func TestR3RunnerContainsSafetyInvariants(t *testing.T) {
	script := readR3Runner(t)
	assertScriptContains(t, script,
		`--request-timeout="$broker_query_timeout"`,
		`-max-ack-gap=`,
		`limits:{cpu:"1"`,
		`nodeSelector:{"kubernetes.io/hostname":$node}`,
		`monitor_cluster_health "$health_monitor_stop"`,
		`wait_for_meta_idle pre-create`,
		`stop_publisher_pods`,
		`start_sli_monitors`,
		`.[-1].rpc_p99_samples >= $min`,
		`R3_SLI_ONLY`,
		`automountServiceAccountToken:false`,
		`tls://nats-leaf-local.production.svc.cluster.local:4222`,
		`stream_topologies`,
		`--since=5m`,
		`qualifier_batch=$(jq -er '.batch_size'`,
		`canary_rates=${R3_CANARY_RATES:-"12000 30000 60000 90000"}`,
	)
	require.NotContains(t, script, `nodeName:$node`)
}

func readR3Runner(t *testing.T) string {
	t.Helper()
	script, err := os.ReadFile("r3-120k.sh")
	require.NoError(t, err)
	return string(script)
}

func assertScriptContains(t *testing.T, script string, required ...string) {
	t.Helper()
	for _, invariant := range required {
		require.Contains(t, script, invariant, "missing safety invariant")
	}
}

func TestR3MessageDistributionRunsUnderNounset(t *testing.T) {
	script, err := os.ReadFile("r3-120k.sh")
	require.NoError(t, err)
	function := extractShellFunction(t, string(script), "messages_for_node")
	command := function + `
set -euo pipefail
[[ $(messages_for_node 10 0) == 3 ]]
[[ $(messages_for_node 10 1) == 3 ]]
[[ $(messages_for_node 10 2) == 4 ]]
[[ $(messages_for_node 720000 0) == 240000 ]]
`
	output, err := exec.Command("bash", "-u", "-c", command).CombinedOutput()
	require.NoError(t, err, string(output))
}

func TestR3SLIOnlySummaryReportsNearestRankDistributions(t *testing.T) {
	script, err := os.ReadFile("r3-120k.sh")
	require.NoError(t, err)
	function := extractShellFunction(t, string(script), "write_sli_summary")
	dir := t.TempDir()
	input := strings.Join([]string{
		`{"rpc":[{"rtt_ms":1},{"rtt_ms":2}],"ingress":{"rtt_ms":3},"valkey":{"ping_rtt_ms":4,"set_rtt_ms":5,"get_rtt_ms":6},"passed":true}`,
		`{"rpc":[{"rtt_ms":7},{"rtt_ms":8}],"ingress":{"rtt_ms":9},"valkey":{"ping_rtt_ms":10,"set_rtt_ms":11,"get_rtt_ms":12},"passed":true}`,
	}, "\n")
	inputPath := dir + "/node2.jsonl"
	require.NoError(t, os.WriteFile(inputPath, []byte(input), 0o600))
	command := function + `
set -euo pipefail
nodes=(node2)
sli_monitor_files=("$INPUT")
results_dir="$RESULTS"
write_sli_summary >/dev/null
`
	cmd := exec.Command("bash", "-c", command)
	cmd.Env = append(os.Environ(), "INPUT="+inputPath, "RESULTS="+dir)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	data, err := os.ReadFile(dir + "/sli-summary.json")
	require.NoError(t, err)
	var summary []struct {
		RPCSamples int `json:"rpc_samples"`
		RPC        struct {
			Min float64 `json:"min"`
			P50 float64 `json:"p50"`
			P95 float64 `json:"p95"`
			P99 float64 `json:"p99"`
			Max float64 `json:"max"`
		} `json:"rpc_health_round_trip_ms"`
		Passed bool `json:"passed"`
	}
	require.NoError(t, json.Unmarshal(data, &summary))
	require.Len(t, summary, 1)
	require.Equal(t, 4, summary[0].RPCSamples)
	require.Equal(t, 1.0, summary[0].RPC.Min)
	require.Equal(t, 2.0, summary[0].RPC.P50)
	require.Equal(t, 8.0, summary[0].RPC.P95)
	require.Equal(t, 8.0, summary[0].RPC.P99)
	require.Equal(t, 8.0, summary[0].RPC.Max)
	require.True(t, summary[0].Passed)
}

func extractShellFunction(t *testing.T, source, name string) string {
	t.Helper()
	marker := name + "() {"
	start := strings.Index(source, marker)
	require.NotEqual(t, -1, start, "missing shell function %s", name)
	end := strings.Index(source[start:], "\n}\n")
	require.NotEqual(t, -1, end, "unterminated shell function %s", name)
	return source[start : start+end+2]
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

func TestBenchmarkContextEnforcesRunTimeout(t *testing.T) {
	ctx, cancel := benchmarkContext(config{runTimeout: time.Millisecond})
	defer cancel()
	select {
	case <-ctx.Done():
		require.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("benchmark context ignored its run timeout")
	}
}

func TestBenchmarkConnectionsDisableReconnectBuffering(t *testing.T) {
	url := runMinimalNATSServer(t)
	nc, err := nats.Connect(url, connectionOptions(config{}, nil, "test", &connectionStats{})...)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	// This must be asserted after Connect: nats.go replaces zero with its
	// default 8 MiB reconnect buffer while constructing the connection.
	require.Equal(t, -1, nc.Opts.ReconnectBufSize)
}

func runMinimalNATSServer(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	go serveMinimalNATS(listener)
	return "nats://" + listener.Addr().String()
}

func serveMinimalNATS(listener net.Listener) {
	conn, err := listener.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	if err := writeMinimalNATSInfo(conn, listener); err != nil {
		return
	}
	answerNATSPings(conn)
}

func writeMinimalNATSInfo(conn net.Conn, listener net.Listener) error {
	port := listener.Addr().(*net.TCPAddr).Port
	_, err := fmt.Fprintf(conn,
		"INFO {\"server_id\":\"test\",\"version\":\"2.14.0\",\"proto\":1,\"host\":\"127.0.0.1\",\"port\":%d,\"max_payload\":1048576}\r\n",
		port,
	)
	return err
}

func answerNATSPings(conn net.Conn) {
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		if scanner.Text() == "PING" {
			_, _ = conn.Write([]byte("PONG\r\n"))
		}
	}
}

func TestAcknowledgementGapTracksRecoveredStalls(t *testing.T) {
	counters := &benchmarkCounters{}
	counters.beginAckWindows(time.Now(), 2)
	counters.recordAcknowledgedAt(0, 10, 100*time.Millisecond)
	counters.recordAcknowledgedAt(0, 10, 250*time.Millisecond)
	counters.recordAcknowledgedAt(0, 10, 2*time.Second)
	counters.recordAcknowledgedAt(1, 10, 100*time.Millisecond)
	finished := counters.started.Add(2 * time.Second)
	counters.finishAckWindows(finished)

	require.Equal(t, int64(40), counters.acked.Load())
	require.Equal(t, 1900*time.Millisecond, counters.maximumAckGap())
	result := result{
		Acknowledged:            40,
		MaxPublishProgressGapMS: durationMS(counters.maximumAckGap()),
	}
	err := validateBenchmark(config{messages: 40, mode: "atomic", maxAckGap: time.Second}, result, nil)
	require.ErrorContains(t, err, "maximum publisher completion-progress gap")
}

func TestFastPublisherCountsFlowAckDeltas(t *testing.T) {
	counters := &benchmarkCounters{}
	counters.beginAckWindows(time.Now(), 1)
	session := fastPublishSession{
		job: publishJob{publisher: 0, count: 100, counters: counters},
	}

	session.recordAckSequenceAt(1, 10*time.Millisecond)
	session.recordAckSequenceAt(64, 20*time.Millisecond)
	session.recordAckSequenceAt(64, 30*time.Millisecond)
	session.recordAckSequenceAt(120, 40*time.Millisecond)
	counters.finishAckWindows(counters.started.Add(50 * time.Millisecond))

	require.Equal(t, uint64(100), session.acked)
	require.Equal(t, int64(100), counters.acked.Load())
	require.Equal(t, 20*time.Millisecond, counters.maximumAckGap())
}

func TestAsyncDrainObservesResolvedPubAcksBeforeWindowFills(t *testing.T) {
	counters := &benchmarkCounters{}
	counters.beginAckWindows(time.Now(), 1)
	job := publishJob{publisher: 0, counters: counters}
	ready := resolvedPubAckFuture()
	pending := unresolvedPubAckFuture()

	remaining, err := drainReadyAcknowledgements(job, []pendingPublish{
		{ok: ready.Ok(), err: ready.Err(), sequence: 0},
		{ok: pending.Ok(), err: pending.Err(), sequence: 1},
	})
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	require.Equal(t, messageSequence(1), remaining[0].sequence)
	require.Equal(t, int64(1), counters.acked.Load())
}

func TestAsyncDrainPropagatesReadyPubAckFailure(t *testing.T) {
	counters := &benchmarkCounters{}
	counters.beginAckWindows(time.Now(), 1)
	failed := failedPubAckFuture(nats.ErrTimeout)

	remaining, err := drainReadyAcknowledgements(
		publishJob{publisher: 0, counters: counters},
		[]pendingPublish{{ok: failed.Ok(), err: failed.Err(), sequence: 7}},
	)
	require.Nil(t, remaining)
	require.ErrorContains(t, err, "PubAck 7")
	require.Equal(t, int64(1), counters.failures.Load())
	require.Equal(t, int64(1), counters.timeouts.Load())
}

func TestSampledPubAckMaxRejectsARecoveredLatencyStall(t *testing.T) {
	result := result{Acknowledged: 40, PubAckMaxMS: 2_001}
	err := validateBenchmark(config{messages: 40, maxAckGap: 2 * time.Second}, result, nil)
	require.ErrorContains(t, err, "maximum sampled PubAck RTT")
}

type stubPubAckFuture struct {
	ok  chan *nats.PubAck
	err chan error
}

func resolvedPubAckFuture() *stubPubAckFuture {
	future := unresolvedPubAckFuture()
	future.ok <- &nats.PubAck{}
	return future
}

func failedPubAckFuture(err error) *stubPubAckFuture {
	future := unresolvedPubAckFuture()
	future.err <- err
	return future
}

func unresolvedPubAckFuture() *stubPubAckFuture {
	return &stubPubAckFuture{
		ok:  make(chan *nats.PubAck, 1),
		err: make(chan error, 1),
	}
}

func (f *stubPubAckFuture) Ok() <-chan *nats.PubAck { return f.ok }
func (f *stubPubAckFuture) Err() <-chan error       { return f.err }
func (f *stubPubAckFuture) Msg() *nats.Msg          { return nil }

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
