package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	valkey_go "github.com/valkey-io/valkey-go"
)

func TestSLIConfigValidation(t *testing.T) {
	valid := testSLIConfig()
	require.NoError(t, validateSLIConfig(valid))

	for name, mutate := range map[string]func(*config){
		"no services":       func(cfg *config) { cfg.sliServices = nil },
		"wildcard service":  func(cfg *config) { cfg.sliServices = []string{"users.*"} },
		"duplicate service": func(cfg *config) { cfg.sliServices = []string{"users", "users"} },
		"zero duration":     func(cfg *config) { cfg.sliDuration = 0 },
		"zero interval":     func(cfg *config) { cfg.sliInterval = 0 },
		"zero timeout":      func(cfg *config) { cfg.sliTimeout = 0 },
		"max above timeout": func(cfg *config) { cfg.sliMaxRTT = cfg.sliTimeout + time.Millisecond },
		"ingress max zero":  func(cfg *config) { cfg.sliIngressMaxRTT = 0 },
		"ingress max high":  func(cfg *config) { cfg.sliIngressMaxRTT = cfg.sliTimeout + time.Millisecond },
		"RPC p99 max zero":  func(cfg *config) { cfg.sliRPCP99Max = 0 },
		"RPC p99 max high":  func(cfg *config) { cfg.sliRPCP99Max = cfg.sliMaxRTT + time.Millisecond },
		"RPC samples zero":  func(cfg *config) { cfg.sliRPCP99Min = 0 },
		"RPC samples high":  func(cfg *config) { cfg.sliRPCP99Min = sliRPCTailWindowSamples + 1 },
		"unsafe key":        func(cfg *config) { cfg.sliKey = "production:key" },
		"write subject":     func(cfg *config) { cfg.sliIngressSubject = "twitch.ingress.admin.shards.scale" },
		"missing NATS":      func(cfg *config) { cfg.sliNATSURL = "" },
		"missing Valkey":    func(cfg *config) { cfg.valkeyAddress = "" },
		"missing password":  func(cfg *config) { cfg.valkeyPassword = "" },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := valid
			cfg.sliServices = append([]string(nil), valid.sliServices...)
			mutate(&cfg)
			require.Error(t, validateSLIConfig(cfg))
		})
	}
}

func TestSLIModeBypassesJetStreamSetup(t *testing.T) {
	cfg := testSLIConfig()
	cfg.stream = "TWITCH_INGRESS"
	cfg.subject = "twitch.ingress.>"
	run, err := newAcceptanceRunForConfig(cfg)
	require.NoError(t, err)
	require.Nil(t, run.setup.nc)
	require.Equal(t, "rpc", run.hub.label)
	require.Equal(t, cfg.sliNATSURL, run.hub.url)
}

func TestRPCHealthReplyValidation(t *testing.T) {
	require.NoError(t, validateRPCHealthReply("sesame", []byte(`{"service":"sesame","ok":true}`)))
	require.ErrorContains(t, validateRPCHealthReply("sesame", []byte(`{"service":"outgress","ok":true}`)), "invalid reply")
	require.ErrorContains(t, validateRPCHealthReply("sesame", []byte(`{"service":"sesame","ok":false}`)), "invalid reply")
	require.ErrorContains(t, validateRPCHealthReply("sesame", []byte(`not-json`)), "malformed JSON")
}

func TestIngressSnapshotHasASeparateTailCeiling(t *testing.T) {
	cfg := testSLIConfig()
	cfg.sliMaxRTT = 250 * time.Millisecond
	cfg.sliIngressMaxRTT = 500 * time.Millisecond
	rtt := 300 * time.Millisecond
	require.Error(t, validateSLIRTT("ordinary RPC", rtt, cfg.sliMaxRTT))
	require.NoError(t, validateSLIRTT("ingress shard snapshot", rtt, cfg.sliIngressMaxRTT))
}

func TestRPCTailGateUsesNearestRankP99(t *testing.T) {
	cfg := testSLIConfig()
	cfg.sliRPCP99Max = 8 * time.Millisecond
	cfg.sliRPCP99Min = 330

	passing := append(rpcSamples(327, 4), rpcSamples(3, 9)...)
	p99, count, err := (&sliRPCTailGate{}).observe(passing, cfg)
	require.NoError(t, err)
	require.Equal(t, 330, count)
	require.Equal(t, 4.0, p99)

	failing := append(rpcSamples(326, 4), rpcSamples(4, 9)...)
	p99, count, err = (&sliRPCTailGate{}).observe(failing, cfg)
	require.ErrorContains(t, err, "rolling RPC p99")
	require.Equal(t, 330, count)
	require.Equal(t, 9.0, p99)
}

func TestRPCTailGateWaitsForMinimumSamplesAndBoundsItsWindow(t *testing.T) {
	cfg := testSLIConfig()
	cfg.sliRPCP99Max = 8 * time.Millisecond
	cfg.sliRPCP99Min = 330
	gate := &sliRPCTailGate{}

	_, count, err := gate.observe(rpcSamples(329, 9), cfg)
	require.NoError(t, err)
	require.Equal(t, 329, count)

	_, _, err = gate.observe(rpcSamples(1, 9), cfg)
	require.ErrorContains(t, err, "exceeds 8.000ms")

	_, count, err = gate.observe(rpcSamples(sliRPCTailWindowSamples, 4), cfg)
	require.NoError(t, err)
	require.Equal(t, sliRPCTailWindowSamples, count)
}

func TestIngressSnapshotRequiresStableConnectedBoundDesiredShards(t *testing.T) {
	tracker := &ingressAttemptTracker{}
	first := healthyIngressSnapshot(2, 0)
	report, err := tracker.validate(first, defaultIngressSLISubject, 4*time.Millisecond)
	require.NoError(t, err)
	require.Equal(t, 2, report.DesiredCount)
	require.Equal(t, "running", report.ConduitManagerState)

	_, err = tracker.validate(healthyIngressSnapshot(2, 1), defaultIngressSLISubject, 4*time.Millisecond)
	require.ErrorContains(t, err, "want zero")

	tracker = &ingressAttemptTracker{}
	require.NoError(t, validateIngressSnapshotForTest(tracker, healthyIngressSnapshot(1, 0)))
	changedSession := healthyIngressSnapshot(1, 0)
	var changed map[string]any
	require.NoError(t, json.Unmarshal(changedSession, &changed))
	changed["shards"].([]any)[0].(map[string]any)["session_id"] = "changed"
	changedSession, err = json.Marshal(changed)
	require.NoError(t, err)
	_, err = tracker.validate(changedSession, defaultIngressSLISubject, time.Millisecond)
	require.ErrorContains(t, err, "session_id changed")

	for name, payload := range map[string][]byte{
		"manager down": []byte(`{"conduit_manager":{"state":"down"},"desired_count":1,"shards":[]}`),
		"unregistered": withIngressShardState(t, "unregistered", true, false),
		"not bound":    withIngressShardState(t, "connected", false, false),
		"handshake":    withIngressShardState(t, "connected", true, true),
		"stale frame":  staleIngressSnapshot(t),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := (&ingressAttemptTracker{}).validate(payload, defaultIngressSLISubject, time.Millisecond)
			require.Error(t, err)
		})
	}
}

func TestIngressSnapshotRejectsRebindBetweenSamples(t *testing.T) {
	for name, mutate := range map[string]func(map[string]any){
		"session changed": func(shard map[string]any) {
			shard["session_id"] = "replacement-session"
		},
		"bound time changed": func(shard map[string]any) {
			shard["bound_at"] = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
		},
	} {
		t.Run(name, func(t *testing.T) {
			tracker := &ingressAttemptTracker{}
			require.NoError(t, validateIngressSnapshotForTest(tracker, healthyIngressSnapshot(2, 0)))

			changed := mutateIngressShard(t, healthyIngressSnapshot(2, 0), 0, mutate)
			_, err := tracker.validate(changed, defaultIngressSLISubject, time.Millisecond)
			require.ErrorContains(t, err, "changed between samples")
		})
	}
}

func TestIngressSnapshotRejectsDesiredCountChangeDuringQualification(t *testing.T) {
	tracker := &ingressAttemptTracker{}
	require.NoError(t, validateIngressSnapshotForTest(tracker, healthyIngressSnapshot(2, 0)))

	scaled := mutateIngressShard(t, healthyIngressSnapshot(3, 0), 0, func(shard map[string]any) {
		shard["session_id"] = "replacement-after-scale"
		shard["bound_at"] = time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	})
	_, err := tracker.validate(scaled, defaultIngressSLISubject, time.Millisecond)
	require.ErrorContains(t, err, "desired_count changed between samples")
}

func TestIngressSnapshotRequiresStableIdentityFields(t *testing.T) {
	for name, field := range map[string]string{
		"missing session":    "session_id",
		"missing bound time": "bound_at",
	} {
		t.Run(name, func(t *testing.T) {
			payload := mutateIngressShard(t, healthyIngressSnapshot(1, 0), 0, func(shard map[string]any) {
				delete(shard, field)
			})
			_, err := (&ingressAttemptTracker{}).validate(payload, defaultIngressSLISubject, time.Millisecond)
			require.ErrorContains(t, err, field)
		})
	}
}

func TestCollectSLISampleValidatesRPCIngressAndValkey(t *testing.T) {
	cfg := testSLIConfig()
	cfg.sliServices = []string{"ingress", "sesame"}
	var stored string
	probes := &sliProbes{
		request: func(_ context.Context, subject string) ([]byte, error) {
			switch subject {
			case "bagel.rpc.health.ingress":
				return []byte(`{"service":"ingress","ok":true}`), nil
			case "bagel.rpc.health.sesame":
				return []byte(`{"service":"sesame","ok":true}`), nil
			case defaultIngressSLISubject:
				return healthyIngressSnapshot(2, 0), nil
			default:
				return nil, fmt.Errorf("unexpected subject %s", subject)
			}
		},
		ping: func(context.Context) (string, error) { return "PONG", nil },
		set: func(_ context.Context, _ string, value string, _ time.Duration) (string, error) {
			stored = value
			return "OK", nil
		},
		get:     func(context.Context, string) (string, error) { return stored, nil },
		healthy: func() error { return nil },
	}
	sample, err := collectSLISample(context.Background(), cfg, probes, &ingressAttemptTracker{}, 1)
	require.NoError(t, err)
	require.Len(t, sample.RPC, 2)
	require.NotNil(t, sample.Ingress)
	require.NotNil(t, sample.Valkey)
	require.Positive(t, sample.DurationMS)
	require.Equal(t, int64((30 * time.Second).Milliseconds()), sample.Valkey.KeyTTLMS)
}

func TestCollectSLISampleFailsOnValkeyMismatch(t *testing.T) {
	cfg := testSLIConfig()
	cfg.sliServices = []string{"sesame"}
	cfg.sliMaxRTT = 10 * time.Millisecond
	probes := healthyTestProbes()
	getCalls := 0
	probes.get = func(context.Context, string) (string, error) {
		getCalls++
		return "stale", nil
	}
	_, err := collectSLISample(context.Background(), cfg, probes, &ingressAttemptTracker{}, 1)
	require.ErrorContains(t, err, "value mismatch")
	require.Greater(t, getCalls, 1, "node-local GET was not retried")
}

func TestSampleValkeyWaitsForNodeLocalReplicaConvergence(t *testing.T) {
	cfg := testSLIConfig()
	cfg.sliMaxRTT = 50 * time.Millisecond
	probes := healthyTestProbes()
	convergedGet := probes.get
	getCalls := 0
	probes.get = func(ctx context.Context, key string) (string, error) {
		getCalls++
		if getCalls == 1 {
			return "", valkey_go.Nil
		}
		if getCalls == 2 {
			return "stale", nil
		}
		return convergedGet(ctx, key)
	}

	sample, maximum, err := sampleValkey(context.Background(), cfg, probes, 1, 30*time.Second)
	require.NoError(t, err)
	require.Equal(t, 3, getCalls)
	require.Positive(t, sample.GetRTT)
	require.Equal(t, durationMilliseconds(maximum), sample.GetRTT)
}

func TestSampleValkeyRejectsConvergencePastMaxRTT(t *testing.T) {
	cfg := testSLIConfig()
	cfg.sliMaxRTT = 5 * time.Millisecond
	cfg.sliTimeout = 50 * time.Millisecond
	probes := healthyTestProbes()
	convergedGet := probes.get
	probes.get = func(ctx context.Context, key string) (string, error) {
		time.Sleep(10 * time.Millisecond)
		return convergedGet(ctx, key)
	}

	sample, _, err := sampleValkey(context.Background(), cfg, probes, 1, 30*time.Second)
	require.ErrorContains(t, err, "Valkey GET RTT")
	require.ErrorContains(t, err, "exceeded max-rtt")
	require.Greater(t, sample.GetRTT, durationMilliseconds(cfg.sliMaxRTT))
}

func TestCollectSLISampleFailsOnTimeoutAndMaxRTT(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		cfg := testSLIConfig()
		cfg.sliServices = []string{"sesame"}
		cfg.sliTimeout = 5 * time.Millisecond
		cfg.sliMaxRTT = time.Millisecond
		probes := healthyTestProbes()
		probes.request = func(ctx context.Context, _ string) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		_, err := collectSLISample(context.Background(), cfg, probes, &ingressAttemptTracker{}, 1)
		require.ErrorContains(t, err, "timed out")
	})

	t.Run("max RTT", func(t *testing.T) {
		cfg := testSLIConfig()
		cfg.sliServices = []string{"sesame"}
		cfg.sliMaxRTT = time.Nanosecond
		probes := healthyTestProbes()
		_, err := collectSLISample(context.Background(), cfg, probes, &ingressAttemptTracker{}, 1)
		require.ErrorContains(t, err, "exceeded max-rtt")
	})
}

func TestContinuousSLIFailsImmediatelyOnNATSConnectionEvent(t *testing.T) {
	cfg := testSLIConfig()
	cfg.sliServices = []string{"sesame"}
	cfg.sliDuration = time.Minute
	cfg.sliInterval = time.Minute
	failures := make(chan error, 1)
	failures <- errors.New("NATS disconnected")
	probes := healthyTestProbes()
	probes.failures = failures
	probes.request = func(ctx context.Context, _ string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	var output bytes.Buffer
	started := time.Now()
	err := runContinuousSLI(context.Background(), cfg, probes, &output)
	require.ErrorContains(t, err, "NATS disconnected")
	require.Less(t, time.Since(started), time.Second)

	var sample sliSample
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(output.Bytes()), &sample))
	require.False(t, sample.Passed)
	require.Contains(t, sample.Failure, "NATS disconnected")
}

func TestConnectionStatsReportsReconnectDisconnectAndAsyncError(t *testing.T) {
	for name, trigger := range map[string]func(*connectionStats){
		"reconnect":  func(stats *connectionStats) { stats.recordReconnect(nil) },
		"disconnect": func(stats *connectionStats) { stats.recordDisconnect(nil, errors.New("gone")) },
		"async":      func(stats *connectionStats) { stats.recordAsyncError(nil, nil, errors.New("bad subscription")) },
	} {
		t.Run(name, func(t *testing.T) {
			stats := &connectionStats{failures: make(chan error, 1)}
			trigger(stats)
			select {
			case err := <-stats.failures:
				require.Error(t, err)
			case <-time.After(time.Second):
				t.Fatal("connection failure was not reported")
			}
		})
	}
}

func testSLIConfig() config {
	return config{
		sliOnly:           true,
		sliServices:       parseSLIServices(defaultSLIServices),
		sliDuration:       time.Second,
		sliInterval:       10 * time.Millisecond,
		sliTimeout:        time.Second,
		sliMaxRTT:         500 * time.Millisecond,
		sliIngressMaxRTT:  500 * time.Millisecond,
		sliRPCP99Max:      8 * time.Millisecond,
		sliRPCP99Min:      330,
		sliKey:            "acceptance:sli:test:run",
		sliIngressSubject: defaultIngressSLISubject,
		sliNATSURL:        "nats://localhost:4222",
		valkeyAddress:     "localhost:6379",
		valkeyPassword:    "test",
		insecureLocal:     true,
		producerID:        "test",
	}
}

func rpcSamples(count int, rttMS float64) []sliRPCSample {
	samples := make([]sliRPCSample, count)
	for i := range samples {
		samples[i] = sliRPCSample{Service: "sesame", RTTMS: rttMS}
	}
	return samples
}

func healthyTestProbes() *sliProbes {
	var stored string
	return &sliProbes{
		request: func(_ context.Context, subject string) ([]byte, error) {
			service := strings.TrimPrefix(subject, "bagel.rpc.health.")
			if service == subject {
				return healthyIngressSnapshot(1, 0), nil
			}
			return []byte(fmt.Sprintf(`{"service":%q,"ok":true}`, service)), nil
		},
		ping: func(context.Context) (string, error) { return "PONG", nil },
		set: func(_ context.Context, _ string, value string, _ time.Duration) (string, error) {
			stored = value
			return "OK", nil
		},
		get:     func(context.Context, string) (string, error) { return stored, nil },
		healthy: func() error { return nil },
	}
}

func healthyIngressSnapshot(desired, attempts int) []byte {
	shards := make([]map[string]any, 0, desired)
	for id := 0; id < desired; id++ {
		shards = append(shards, map[string]any{
			"shard_id":            id,
			"state":               "connected",
			"session_id":          fmt.Sprintf("session-%d", id),
			"bound":               true,
			"bound_at":            time.Date(2026, 1, 1, 0, 0, id, 0, time.UTC),
			"handshake_in_flight": false,
			"keepalive_ms":        10_000,
			"attempts":            attempts,
			"last_frame_at":       time.Now().UTC(),
		})
	}
	payload, err := json.Marshal(map[string]any{
		"conduit_manager": map[string]any{"state": "running"},
		"desired_count":   desired,
		"shards":          shards,
	})
	if err != nil {
		panic(err)
	}
	return payload
}

func validateIngressSnapshotForTest(tracker *ingressAttemptTracker, payload []byte) error {
	_, err := tracker.validate(payload, defaultIngressSLISubject, time.Millisecond)
	return err
}

func mutateIngressShard(
	t *testing.T,
	payload []byte,
	shardID int,
	mutate func(map[string]any),
) []byte {
	t.Helper()
	var snapshot map[string]any
	require.NoError(t, json.Unmarshal(payload, &snapshot))
	shards := snapshot["shards"].([]any)
	mutate(shards[shardID].(map[string]any))
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	return encoded
}

func withIngressShardState(t *testing.T, state string, bound, handshake bool) []byte {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(healthyIngressSnapshot(1, 0), &payload))
	shard := payload["shards"].([]any)[0].(map[string]any)
	shard["state"] = state
	shard["bound"] = bound
	shard["handshake_in_flight"] = handshake
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	return encoded
}

func staleIngressSnapshot(t *testing.T) []byte {
	t.Helper()
	var payload map[string]any
	require.NoError(t, json.Unmarshal(healthyIngressSnapshot(1, 0), &payload))
	shard := payload["shards"].([]any)[0].(map[string]any)
	shard["last_frame_at"] = time.Now().Add(-time.Minute).UTC()
	encoded, err := json.Marshal(payload)
	require.NoError(t, err)
	return encoded
}
