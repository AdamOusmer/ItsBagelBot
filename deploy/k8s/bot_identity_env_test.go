// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"slices"
	"testing"
)

func TestUsersReadsBotIdentityFromDoppler(t *testing.T) {
	manifest, found := findDeployment(t, "users.yaml", "users")
	if !found {
		t.Fatal("users Deployment is missing from users.yaml")
	}
	if slices.Contains(envNames(manifest), "TWITCH_BOT_USER_ID") {
		t.Error("users sets TWITCH_BOT_USER_ID inline, which overrides the users-env Doppler value")
	}
	if !envFromSecret(manifest, "users-env") {
		t.Error("users lost envFrom users-env, its only TWITCH_BOT_USER_ID source")
	}
}

func envFromSecret(manifest envManifest, secret string) bool {
	for _, c := range manifest.Spec.Template.Spec.Containers {
		if slices.ContainsFunc(c.EnvFrom, func(source any) bool { return secretRefName(source) == secret }) {
			return true
		}
	}
	return false
}

func secretRefName(source any) any {
	entry, _ := source.(map[string]any)
	ref, _ := entry["secretRef"].(map[string]any)
	return ref["name"]
}
