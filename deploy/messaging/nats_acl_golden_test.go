// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"flag"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

var updateACLGolden = flag.Bool("update", false, "regenerate accounts.yaml from nats-auth.conf")

const accountsYAMLPath = "accounts.yaml"

func TestAccountsYAMLMatchesNatsAuthConf(t *testing.T) {
	got, err := yaml.Marshal(aclFromNatsAuthConf(t))
	if err != nil {
		t.Fatal(err)
	}
	got = append(licenseHeaderYAML, got...)

	if *updateACLGolden {
		if err := os.WriteFile(accountsYAMLPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(accountsYAMLPath)
	if err != nil {
		t.Fatalf("read %s: %v (regenerate with: go test ./deploy/messaging -run TestAccountsYAMLMatchesNatsAuthConf -update)", accountsYAMLPath, err)
	}
	if string(got) != string(want) {
		t.Fatalf("%s is stale; regenerate with: go test ./deploy/messaging -run TestAccountsYAMLMatchesNatsAuthConf -update\n--- got ---\n%s\n--- want ---\n%s", accountsYAMLPath, got, want)
	}
}

var licenseHeaderYAML = []byte("# Copyright (c) 2026 Adam Ousmer. All rights reserved.\n# Proprietary. No license granted. See LICENSE.md.\n\n")
