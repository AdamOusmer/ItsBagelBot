package k8s

import (
	"regexp"
	"strings"
	"testing"
)

var serviceSubjectPattern = regexp.MustCompile(`\{ service:.*(?:subject: )?"([^"]+)"`)

func TestExactRPCGrantsIncludeNodeLocalVariant(t *testing.T) {
	config := sourceFile{name: "nats-auth.conf"}.read(t)
	counts := make(map[string]int)
	for _, line := range strings.Split(config, "\n") {
		match := serviceSubjectPattern.FindStringSubmatch(line)
		if len(match) == 2 {
			counts[match[1]]++
		}
	}

	checked := 0
	for subject, count := range counts {
		if strings.HasSuffix(subject, ".>") || strings.HasSuffix(subject, ".node.*") {
			continue
		}
		checked++
		local := subject + ".node.*"
		if counts[local] < count {
			t.Errorf("exact service grant %q occurs %d times; local grant %q occurs %d times",
				subject, count, local, counts[local])
		}
	}
	if checked < 10 {
		t.Fatalf("checked only %d exact RPC grants; parser likely stopped matching the config", checked)
	}
}
