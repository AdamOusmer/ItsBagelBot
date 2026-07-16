package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type shellScript string

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
	require.NotContains(t, string(script), `"$results_dir"/"$label"-*.json`)
	require.Contains(t, string(script), `"${result_files[@]}"`)
	require.NotContains(t, string(script), `name:"worker-env"`)
	require.NotContains(t, string(script), `key:"VALKEY_ADDR"`)
	require.NotContains(t, string(script), `control_pod=${pods[`)
	script.assertContains(t,
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
	script.assertContains(t,
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
	require.NotContains(t, string(script), `nodeName:$node`)
}

func readR3Runner(t *testing.T) shellScript {
	t.Helper()
	script, err := os.ReadFile("r3-120k.sh")
	require.NoError(t, err)
	return shellScript(script)
}

func (s shellScript) assertContains(t *testing.T, required ...string) {
	t.Helper()
	for _, invariant := range required {
		require.Contains(t, string(s), invariant, "missing safety invariant")
	}
}

func TestR3MessageDistributionRunsUnderNounset(t *testing.T) {
	script, err := os.ReadFile("r3-120k.sh")
	require.NoError(t, err)
	function := shellScript(script).function(t, "messages_for_node")
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
	function := shellScript(script).function(t, "write_sli_summary")
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

func (s shellScript) function(t *testing.T, name string) string {
	t.Helper()
	source := string(s)
	marker := name + "() {"
	start := strings.Index(source, marker)
	require.NotEqual(t, -1, start, "missing shell function %s", name)
	end := strings.Index(source[start:], "\n}\n")
	require.NotEqual(t, -1, end, "unterminated shell function %s", name)
	return source[start : start+end+2]
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
