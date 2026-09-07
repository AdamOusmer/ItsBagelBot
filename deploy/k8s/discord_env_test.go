// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"errors"
	"io"
	"os"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

// envManifest is one manifest seen as "which environment variables does each
// container get", which is the only thing these tests ask of it.
type envManifest struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Template struct {
			Spec struct {
				Containers []struct {
					Env []struct {
						Name string `yaml:"name"`
					} `yaml:"env"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

// deploymentEnvNames lists the env var names one Deployment's containers set.
func deploymentEnvNames(t *testing.T, filename, name string) []string {
	t.Helper()
	manifest, found := findDeployment(t, filename, name)
	if !found {
		t.Fatalf("%s Deployment is missing from %s", name, filename)
	}
	return envNames(manifest)
}

// findDeployment walks one multi-document manifest file for the Deployment
// named name. A file that never names it is (zero, false) rather than a
// t.Fatal here, so the caller owns the message and this stays a plain search.
func findDeployment(t *testing.T, filename, name string) (envManifest, bool) {
	t.Helper()
	f, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	for {
		var manifest envManifest
		err := decoder.Decode(&manifest)
		if errors.Is(err, io.EOF) {
			return envManifest{}, false
		}
		if err != nil {
			t.Fatal(err)
		}
		if manifest.Kind == "Deployment" && manifest.Metadata.Name == name {
			return manifest, true
		}
	}
}

// envNames flattens every container's env var names, in manifest order.
func envNames(manifest envManifest) []string {
	out := []string{}
	for _, c := range manifest.Spec.Template.Spec.Containers {
		for _, e := range c.Env {
			out = append(out, e.Name)
		}
	}
	return out
}

// TestDiscordManifestsCarryNoDeadDataSwitch: DISCORD_DATA_ENABLED used to pick
// between discord-data and a pure-Valkey mode. Both mains now call NewRPC
// unconditionally (internal/discordstore/select.go says why the fallback could
// not survive per-guild configuration), so the variable is read by nothing.
//
// A manifest env var that no process reads is worse than clutter: the next
// person to debug a Discord outage sets it to "false" expecting a degraded
// mode, gets no change at all, and spends the outage looking at the wrong
// layer.
func TestDiscordManifestsCarryNoDeadDataSwitch(t *testing.T) {
	for _, name := range []string{"discord-engine", "discord-outgress", "discord-ingress"} {
		names := deploymentEnvNames(t, "discord.yaml", name)
		if slices.Contains(names, "DISCORD_DATA_ENABLED") {
			t.Errorf("%s still sets DISCORD_DATA_ENABLED, which no main reads", name)
		}
	}
}

// TestDiscordDataPrefixSurvives is the other half: the prefix IS read (both
// mains pass it to discordstore), so removing the switch must not have taken
// it with it.
func TestDiscordDataPrefixSurvives(t *testing.T) {
	for _, name := range []string{"discord-engine", "discord-outgress"} {
		names := deploymentEnvNames(t, "discord.yaml", name)
		if !slices.Contains(names, "NATS_DISCORD_DATA_RPC_PREFIX") {
			t.Errorf("%s lost NATS_DISCORD_DATA_RPC_PREFIX", name)
		}
	}
}
