package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGatewayAcceptanceIsGuardedAndNotReconciled(t *testing.T) {
	kustomization, err := os.ReadFile("../kustomization.yaml")
	require.NoError(t, err)
	require.NotContains(t, string(kustomization), "nats-live-acceptance/gateway")

	cmd := exec.Command("bash", "gateway/local-first.sh")
	cmd.Env = withoutEnv(os.Environ(), "CONFIRM_NATS_GATEWAY_ACCEPTANCE")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	require.Contains(t, string(output), "no actions performed")
	require.Contains(t, string(output), "LOCAL-FIRST-RPC")
}

func TestGatewayAcceptanceModelsLocalCoreAndEdges(t *testing.T) {
	topology := readGatewayFixture(t, "topology.yaml")
	require.Contains(t, topology, "kubernetes.io/hostname: node2")
	require.Contains(t, topology, "kubernetes.io/hostname: node3")
	require.Contains(t, topology, "kubernetes.io/hostname: worker1")
	require.Contains(t, topology, "kubernetes.io/hostname: node1")
	require.Contains(t, topology, "name: nats-gateway-acceptance-isolation")
	require.Contains(t, topology, "name: fleet-ca-issuer")
	require.Contains(t, topology, "server auth")
	require.Contains(t, topology, "client auth")
	require.Contains(t, topology, "priorityClassName: nats-r3-bench-nonpreempting")
	require.NotContains(t, topology, "kind: Secret")
	require.NotContains(t, topology, "nodeName:")
}

func TestGatewayAcceptanceConfigsAreCoreNATSWithVerifiedTLS(t *testing.T) {
	common := readGatewayFixture(t, "common.conf")
	require.Contains(t, common, `include "/etc/nats/auth/auth.conf"`)
	require.Contains(t, common, `min_version: "1.2"`)
	require.NotContains(t, common, "jetstream")
	require.NotContains(t, common, "leafnodes")

	tests := []struct {
		file        string
		clusterName string
		clustered   bool
	}{
		{file: "core-0.conf", clusterName: `name: "rpc-core"`, clustered: true},
		{file: "core-1.conf", clusterName: `name: "rpc-core"`, clustered: true},
		{file: "worker1.conf", clusterName: `name: "rpc-worker1"`},
		{file: "node1.conf", clusterName: `name: "rpc-node1"`},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			config := readGatewayFixture(t, test.file)
			require.Contains(t, config, test.clusterName)
			require.Contains(t, config, `include "/etc/nats/config/common.conf"`)
			require.Contains(t, config, "verify: true")
			require.Contains(t, config, "verify_cert_and_check_known_urls: true")
			require.Contains(t, config, "gateway {")
			require.Equal(t, test.clustered, strings.Contains(config, "cluster {"))
			require.NotContains(t, config, "jetstream")
			require.NotContains(t, config, "leafnodes")
			require.NotContains(t, config, "nats.production.svc")
			require.NotContains(t, config, "nats-leaf")
		})
	}
}

func TestGatewayAcceptanceKeepsEightMillisecondGateAndCleanup(t *testing.T) {
	runner := readGatewayFixture(t, "local-first.sh")
	for _, invariant := range []string{
		`RPC_P99_MAX_MS:-8`,
		`gate > 0 && gate <= 8`,
		`local-priority`,
		`remote-fallback`,
		`kubectl apply -k "$script_dir"`,
		`kubectl delete -k "$script_dir"`,
		`production_nats_baseline`,
		`kind: NetworkPolicy`,
	} {
		require.Contains(t, runner, invariant)
	}
	require.NotContains(t, runner, "go build")
	require.NotContains(t, runner, "docker")
	require.NotContains(t, runner, "podman")
}

func readGatewayFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("gateway", name))
	require.NoError(t, err)
	return string(data)
}
