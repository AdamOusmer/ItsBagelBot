// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRuntimeStreamOwnershipMatchesACL keeps startup reconciliation aligned
// with the identities nats-auth.conf grants STREAM.CREATE/UPDATE to. That
// conf is generated and committed by the recipes repo now (see
// recipes/deploy/README.md and its own TestServiceBusJetStreamPermissions-
// AreExact); this test never reads it — it scans this repo's own
// app/*/main.go files, which is why it stayed here instead of moving with
// the conf-shape tests.
func TestRuntimeStreamOwnershipMatchesACL(t *testing.T) {
	mainFiles, err := filepath.Glob(filepath.Join("..", "..", "app", "*", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	// Each owner's managed-stream shape now comes from its recipes binding's
	// Manages()-style method (see recipes/MAP.md's "manage" rows) instead of a
	// literal []bus.StreamSpec — the binding is generated from the fleet
	// manifest, so it is the single source of truth for who owns what.
	check := streamOwnershipCheck{
		want: map[string]string{
			"users":     "k.Z5G2Z()",
			"sesame":    "k.ZFLOB()",
			"projector": "k.ZNT6V()",
			"outgress":  "k.ZWMQF()",
		},
		seen: make(map[string]bool, 4),
	}

	for _, name := range mainFiles {
		check.inspect(t, sourceFile{name: name})
	}
	for service := range check.want {
		if !check.seen[service] {
			t.Errorf("stream owner %s does not call EnsureStreams", service)
		}
	}
}

type streamOwnershipCheck struct {
	want map[string]string
	seen map[string]bool
}

type sourceFile struct {
	name string
}

func (c *streamOwnershipCheck) inspect(t *testing.T, file sourceFile) {
	t.Helper()
	body := file.read(t)
	if !strings.Contains(body, "bus.EnsureStreams(") {
		return
	}
	service := filepath.Base(filepath.Dir(file.name))
	snippet, ok := c.want[service]
	if !ok {
		t.Errorf("%s reconciles streams but has no stream-owner ACL", service)
		return
	}
	if !strings.Contains(body, snippet) {
		t.Errorf("%s does not reconcile only its owned stream(s): want %s", service, snippet)
	}
	c.seen[service] = true
}

func (f sourceFile) read(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(f.name)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
