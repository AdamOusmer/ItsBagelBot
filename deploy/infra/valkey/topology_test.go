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

	assert.Regexp(t, `(?m)^  replicas: 3$`, statefulSet, "three Valkey+Sentinel pods")
	assert.Contains(t, statefulSet, topologyMarker)
	assert.Contains(t, statefulSet, partitioningGuard)
	assert.Contains(t, statefulSet, "- --sentinel")
	assert.Contains(t, statefulSet, "sentinel monitor myprimary")
	assert.Contains(t, statefulSet, "replica-announce-ip ${POD_FQDN}")
	assert.Contains(t, statefulSet, "sentinel announce-hostnames yes")
	assert.NotContains(t, statefulSet, "status.hostIP", "Valkey must not use the Tailscale-backed Kubernetes host IP")
	assert.Regexp(t, `(?m)^      hostNetwork: true$`, statefulSet, "Valkey TLS must not be re-encrypted by the CNI tunnel")
	assert.Regexp(t, `(?m)^      dnsPolicy: ClusterFirstWithHostNet$`, statefulSet, "host-netns members still resolve cluster DNS identities")
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

// TestPromotionIsUnfenced replaces the old host-allowlist assertions. That
// allowlist gated promotion by hostname, and when the fleet moved onto
// node4/node5/node6 it silently fenced two of the three live members at
// replica-priority 0 — leaving one promotable member, so Sentinel could not fail
// over and a single node loss meant no writable primary. Every member is now a
// candidate and Sentinel's election decides.
func TestPromotionIsUnfenced(t *testing.T) {
	statefulSet := readFile(t, "statefulset.yaml")

	assert.NotRegexp(t, `node[0-9]\|node[0-9]`, statefulSet,
		"no host allowlist may gate promotion or startup")
	assert.NotRegexp(t, `(?m)^\s+echo "replica-priority`, statefulSet,
		"no member may be written a replica-priority; the default makes all eligible")
	assert.Contains(t, statefulSet, `sed -i '/^replica-priority /d' /data/valkey.conf`,
		"a retained config's stale replica-priority must be stripped")
	assert.NotRegexp(t, `(?m)^\s+- name: NODE_NAME$`, statefulSet,
		"placement must not be exposed to the init script at all")
}

// TestColdBootstrapSeedsByOrdinalNotHost pins the one deterministic choice that
// remains. Without it three simultaneously-fresh members would each declare
// themselves primary. The seed is the ordinal precisely because it is
// placement-independent: the previous hostname seed made the whole StatefulSet
// undeployable the moment that node left the fleet.
func TestColdBootstrapSeedsByOrdinalNotHost(t *testing.T) {
	statefulSet := readFile(t, "statefulset.yaml")

	assert.Regexp(t, `(?m)^  podManagementPolicy: Parallel$`, statefulSet)
	assert.Contains(t, statefulSet, `[ "${POD_NAME}" != "valkey-node-0" ]`,
		"ordinal 0 must be the cold-start seed")
	assert.Contains(t, statefulSet, `[ ! -f /data/valkey.conf ]`,
		"only a fresh member waits; a retained member already has a role")
	assert.Contains(t, statefulSet, `sentinel monitor myprimary ${MASTER_ENDPOINT} 6380 2`,
		"Sentinel quorum stays 2 of 3")
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
