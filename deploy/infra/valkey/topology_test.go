package valkeyinfra_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	topologyMarker    = "itsbagelbot.dev/valkey-topology: sentinel-single-primary"
	partitioningGuard = "itsbagelbot.dev/valkey-partitioning: disabled"
)

func TestSentinelSinglePrimaryTopologyIsConfigured(t *testing.T) {
	statefulSet := readFile(t, "statefulset.yaml")
	valkeyConfig := readFile(t, "config/valkey.conf")

	assert.Regexp(t, `(?m)^  replicas: 4$`, statefulSet, "four Valkey+Sentinel pods")
	assert.Contains(t, statefulSet, topologyMarker)
	assert.Contains(t, statefulSet, partitioningGuard)
	assert.Contains(t, statefulSet, "- --sentinel")
	assert.Contains(t, statefulSet, "sentinel monitor myprimary")
	assert.Contains(t, statefulSet, "replica-announce-ip ${POD_FQDN}")
	assert.Contains(t, statefulSet, "sentinel announce-hostnames yes")
	assert.NotContains(t, statefulSet, "status.hostIP", "Valkey must not use the Tailscale-backed Kubernetes host IP")
	assert.NotContains(t, statefulSet, "hostPort:", "Valkey must stay on the pod data plane")
	assert.Regexp(t, `(?m)^        - --cluster-enabled\n        - "no"$`, statefulSet, "authoritative Cluster-mode disable flag")
	assert.Regexp(t, `(?m)^        - --replica-read-only\n        - "yes"$`, statefulSet, "authoritative read-only replica flag")
	assert.Regexp(t, `(?m)^cluster-enabled no$`, valkeyConfig, "Cluster mode disabled in base config")
	assert.Regexp(t, `(?m)^replica-read-only yes$`, valkeyConfig, "read-only replicas in base config")

	allSources := infrastructureSources(t)
	statefulSets := regexp.MustCompile(`(?m)^kind: StatefulSet$`).FindAllStringIndex(allSources, -1)
	assert.Len(t, statefulSets, 1, "one unpartitioned Valkey failover set")
	assert.NotRegexp(t,
		`(?i)cluster-enabled[ =]+yes|cluster-config-file|cluster-announce-|cluster-require-full-coverage|cluster\s+meet`,
		allSources,
		"partitioning configuration must remain disabled",
	)
}

func TestMasterEligibilityIsReconciledOnEveryBoot(t *testing.T) {
	statefulSet := readFile(t, "statefulset.yaml")

	assert.Contains(t, statefulSet, `case "${NODE_NAME}" in`)
	assert.Contains(t, statefulSet, `node2|node3)`)
	assert.Equal(t, 1, strings.Count(statefulSet, `echo "replica-priority 100"`), "node2/node3 are the only eligible nodes")
	assert.Equal(t, 1, strings.Count(statefulSet, `echo "replica-priority 0"`), "all non-allowlisted nodes are fenced")
	assert.NotContains(t, statefulSet, `replica-priority 200`, "node1 must never remain a last-resort master")
}

func TestColdBootstrapCannotMakeAnArbitraryOrdinalPrimary(t *testing.T) {
	statefulSet := readFile(t, "statefulset.yaml")

	assert.Regexp(t, `(?m)^  podManagementPolicy: Parallel$`, statefulSet)
	assert.Contains(t, statefulSet, `elif [ "${CONFIG_PRESENT}" = "false" ] && [ "${NODE_NAME}" != "node2" ]; then`)
	assert.Contains(t, statefulSet, `Waiting for a node2/node3 Sentinel primary`)
	assert.Contains(t, statefulSet, `[ "${FENCED}" = "true" ] && [ "${LIVE_MASTER}" = "${POD_FQDN}" ]`)
	assert.NotContains(t, statefulSet, "POD_INDEX", "StatefulSet ordinal must not confer primary eligibility")
	assert.NotContains(t, statefulSet, "MASTER_ENDPOINT:-valkey-node-0", "pod zero must not be a cold-start fallback")
}

func TestLocalReadServiceRemainsNodeLocal(t *testing.T) {
	services := readFile(t, "services.yaml")
	localService := regexp.MustCompile(`(?s)name: valkey-local\n.*?internalTrafficPolicy: Local\n.*?port: 6380`).FindString(services)
	if localService == "" {
		t.Fatal("valkey-local must retain internalTrafficPolicy: Local on TLS port 6380")
	}
}

func TestRuntimeTuningOverridesRetainedConfig(t *testing.T) {
	statefulSet := readFile(t, "statefulset.yaml")
	baseConfig := readFile(t, "config/valkey.conf")
	tuning := readFile(t, "config/tuning.conf")
	kustomization := readFile(t, "kustomization.yaml")

	assert.Contains(t, baseConfig, "include /config/tuning.conf")
	assert.Contains(t, statefulSet, `sed -i '\|^include /config/tuning.conf$|d' /data/valkey.conf`)
	assert.Contains(t, statefulSet, `echo "include /config/tuning.conf" >> /data/valkey.conf`)
	assert.Contains(t, kustomization, "- config/tuning.conf")
	assert.Regexp(t, `(?m)^appendfsync everysec$`, tuning)
	assert.Regexp(t, `(?m)^repl-backlog-size 128mb$`, tuning)
	assert.Regexp(t, `(?m)^min-replicas-to-write 1$`, tuning)
}

func infrastructureSources(t *testing.T) string {
	t.Helper()
	paths := append(glob(t, "*.yaml"), glob(t, "config/*.conf")...)
	var sources strings.Builder
	for _, path := range paths {
		sources.WriteString("\n# source: " + filepath.ToSlash(path) + "\n")
		sources.WriteString(readFile(t, path))
	}
	return sources.String()
}

func glob(t *testing.T, pattern string) []string {
	t.Helper()
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %q: %v", pattern, err)
	}
	return paths
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}
