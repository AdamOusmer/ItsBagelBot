package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestR3RunnerDefaultsToNodeLocalPublishAndCapturesBrokerPressure(t *testing.T) {
	script, err := os.ReadFile("r3-120k.sh")
	require.NoError(t, err)
	body := string(script)
	for _, required := range []string{
		`publish_target=${R3_PUBLISH_TARGET:-local}`,
		`publisher_url_for_node`,
		`proxy/routez?subs=0`,
		`broker_metrics`,
		`max_broker_cpu_pct`,
		`trial_max_p99_ms`,
	} {
		require.Contains(t, body, required)
	}
	require.NotContains(t, body, `-hub-url="$leader_url" -domain= -replicas=3 -required-peers=3 \
      -stream "$current_stream" -subject "$current_subject" \
      -create-stream=false -cleanup=false \
      -producer-id=`)
}

func TestIsolatedR3RunnerCapturesBrokerAndRouteDiagnostics(t *testing.T) {
	script, err := os.ReadFile("r3-isolated-tune.sh")
	require.NoError(t, err)
	body := string(script)
	for _, required := range []string{
		`broker_metrics_snapshot`,
		`proxy/routez?subs=0`,
		`peak_route_pending_bytes`,
		`peak_follower_lag`,
		`R3_ISOLATED_BROKER_CPU_MAX_PCT`,
	} {
		require.Contains(t, body, required)
	}
}

func TestR3MatrixReportQualifiesOnlyTheFullOperatingGate(t *testing.T) {
	dir := t.TempDir()
	qualified := matrixFixture(matrixRun{
		target: 90_000, acknowledged: 89_500, durationMS: 1_800_000,
		p99: 1.9, cpu: 74, passed: true,
	})
	qualifiedPath := filepath.Join(dir, "summary.json")
	writeJSON(t, qualifiedPath, qualified)

	cmd := exec.Command("bash", "r3-matrix-report.sh", qualifiedPath)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	var report struct {
		Qualified     bool             `json:"qualified"`
		QualifiedRuns []map[string]any `json:"qualified_runs"`
	}
	require.NoError(t, json.Unmarshal(output, &report))
	require.True(t, report.Qualified)
	require.Len(t, report.QualifiedRuns, 1)

	short := matrixFixture(matrixRun{
		target: 90_000, acknowledged: 89_500, durationMS: 1_799_999,
		p99: 1.9, cpu: 74, passed: true,
	})
	shortPath := filepath.Join(dir, "short.json")
	writeJSON(t, shortPath, short)
	cmd = exec.Command("bash", "r3-matrix-report.sh", shortPath)
	output, err = cmd.CombinedOutput()
	require.Error(t, err, string(output))
	require.Contains(t, string(output), "not yet qualified")

	cmd = exec.Command("bash", "r3-matrix-report.sh", "--report-only", shortPath)
	output, err = cmd.CombinedOutput()
	require.NoError(t, err, string(output))

	tooSlow := matrixFixture(matrixRun{
		target: 90_000, acknowledged: 89_500, durationMS: 1_800_000,
		p99: 2.01, cpu: 74, passed: true,
	})
	tooSlowPath := filepath.Join(dir, "too-slow.json")
	writeJSON(t, tooSlowPath, tooSlow)
	cmd = exec.Command("bash", "r3-matrix-report.sh", tooSlowPath)
	output, err = cmd.CombinedOutput()
	require.Error(t, err, string(output))
	require.Contains(t, string(output), "not yet qualified")
}

type matrixRun struct {
	target       int
	acknowledged float64
	durationMS   int
	p99          float64
	cpu          float64
	passed       bool
}

func matrixFixture(run matrixRun) map[string]any {
	return map[string]any{
		"stream_replicas":               3,
		"publish_target":                "local",
		"publish_mode":                  "async",
		"target_messages_per_second":    run.target,
		"aggregate_messages_per_second": run.acknowledged,
		"conservative_duration_ms":      run.durationMS,
		"worst_node_puback_p50_ms":      1.0,
		"worst_node_puback_p95_ms":      4.0,
		"worst_node_puback_p99_ms":      run.p99,
		"puback_max_ms":                 10.0,
		"broker_metrics": map[string]any{
			"peak_cpu_limit_utilization_pct": run.cpu,
			"peak_memory_bytes":              1_000_000,
			"peak_route_pending_bytes":       0,
			"peak_follower_lag":              0,
		},
		"errors":     0,
		"timeouts":   0,
		"reconnects": 0,
		"passed":     run.passed,
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}
