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
	assert.Contains(t, statefulSet, `echo "replica-priority 0"`)
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

func TestLocalReadServiceRemainsNodeLocal(t *testing.T) {
	services := readFile(t, "services.yaml")
	localService := regexp.MustCompile(`(?s)name: valkey-local\n.*?internalTrafficPolicy: Local\n.*?port: 6380`).FindString(services)
	if localService == "" {
		t.Fatal("valkey-local must retain internalTrafficPolicy: Local on TLS port 6380")
	}
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
