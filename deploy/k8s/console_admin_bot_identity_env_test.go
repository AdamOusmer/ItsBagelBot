// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"slices"
	"testing"
)

func TestConsoleAdminReadsBotIdentityFromDoppler(t *testing.T) {
	manifest, found := findDeployment(t, "console-admin.yaml", "console-admin")
	if !found {
		t.Fatal("console-admin Deployment is missing from console-admin.yaml")
	}
	if slices.Contains(envNames(manifest), "TWITCH_BOT_USER_ID") {
		t.Error("console-admin sets TWITCH_BOT_USER_ID inline, which overrides the console-admin-env Doppler value")
	}
	if !envFromSecret(manifest, "console-admin-env") {
		t.Error("console-admin lost envFrom console-admin-env, its only TWITCH_BOT_USER_ID source")
	}
}

func TestConsoleDashboardPinsBotIdentityToOutgress(t *testing.T) {
	manifest, found := findDeployment(t, "console-dashboard.yaml", "console-dashboard")
	if !found {
		t.Fatal("console-dashboard Deployment is missing from console-dashboard.yaml")
	}
	refs := secretEnvOf(manifest).Refs
	if got, want := refs["TWITCH_BOT_USER_ID"], "outgress-env/TWITCH_BOT_USER_ID"; got != want {
		t.Errorf("console-dashboard TWITCH_BOT_USER_ID source = %q, want %q (shares the outgress bot identity so sign-in cannot swap in a broadcaster grant)", got, want)
	}
}
