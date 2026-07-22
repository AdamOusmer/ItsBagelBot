package k8s

import (
	"regexp"
	"strings"
	"testing"
)

var serviceSubjectPattern = regexp.MustCompile(`\{ service:.*(?:subject: )?"([^"]+)"`)

func TestExactRPCGrantsIncludeNodeLocalVariant(t *testing.T) {
	config := sourceFile{name: "nats-auth.conf"}.read(t)
	counts := serviceGrantCounts(config)
	exact := exactServiceGrants(counts)

	for subject, count := range exact {
		local := subject + ".node.*"
		if counts[local] < count {
			t.Errorf("exact service grant %q occurs %d times; local grant %q occurs %d times",
				subject, count, local, counts[local])
		}
	}
	if len(exact) < 10 {
		t.Fatalf("checked only %d exact RPC grants; parser likely stopped matching the config", len(exact))
	}
}

func serviceGrantCounts(config string) map[string]int {
	counts := make(map[string]int)
	for _, line := range strings.Split(config, "\n") {
		match := serviceSubjectPattern.FindStringSubmatch(line)
		if len(match) == 2 {
			counts[match[1]]++
		}
	}
	return counts
}

func exactServiceGrants(counts map[string]int) map[string]int {
	exact := make(map[string]int)
	for subject, count := range counts {
		if !strings.HasSuffix(subject, ".>") && !strings.HasSuffix(subject, ".node.*") {
			exact[subject] = count
		}
	}
	return exact
}
