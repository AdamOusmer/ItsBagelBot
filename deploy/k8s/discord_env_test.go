// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

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
						Name      string `yaml:"name"`
						ValueFrom struct {
							SecretKeyRef struct {
								Name string `yaml:"name"`
								Key  string `yaml:"key"`
							} `yaml:"secretKeyRef"`
						} `yaml:"valueFrom"`
					} `yaml:"env"`
					EnvFrom []any `yaml:"envFrom"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

func deploymentEnvNames(t *testing.T, filename, name string) []string {
	t.Helper()
	manifest, found := findDeployment(t, filename, name)
	if !found {
		t.Fatalf("%s Deployment is missing from %s", name, filename)
	}
	return envNames(manifest)
}

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

func envNames(manifest envManifest) []string {
	out := []string{}
	for _, c := range manifest.Spec.Template.Spec.Containers {
		for _, e := range c.Env {
			out = append(out, e.Name)
		}
	}
	return out
}

func TestDiscordManifestsCarryNoDeadDataSwitch(t *testing.T) {
	for _, name := range []string{"discord-engine", "discord-outgress", "discord-ingress"} {
		names := deploymentEnvNames(t, "discord.yaml", name)
		if slices.Contains(names, "DISCORD_DATA_ENABLED") {
			t.Errorf("%s still sets DISCORD_DATA_ENABLED, which no main reads", name)
		}
	}
}

func TestDiscordDataPrefixSurvives(t *testing.T) {
	for _, name := range []string{"discord-engine", "discord-outgress"} {
		names := deploymentEnvNames(t, "discord.yaml", name)
		if !slices.Contains(names, "NATS_DISCORD_DATA_RPC_PREFIX") {
			t.Errorf("%s lost NATS_DISCORD_DATA_RPC_PREFIX", name)
		}
	}
}

type secretEnv struct {
	Refs    map[string]string
	EnvFrom int
}

func secretEnvOf(manifest envManifest) secretEnv {
	out := secretEnv{Refs: map[string]string{}}
	for _, c := range manifest.Spec.Template.Spec.Containers {
		out.EnvFrom += len(c.EnvFrom)
		for _, e := range c.Env {
			if ref := e.ValueFrom.SecretKeyRef; ref.Key != "" {
				out.Refs[e.Name] = ref.Name + "/" + ref.Key
			}
		}
	}
	return out
}

func TestDeployerSecretsAreWiredOneByOne(t *testing.T) {
	manifest, found := findDeployment(t, deployerManifest, "deployer")
	if !found {
		t.Fatal("deployer Deployment is missing from deployer.yaml")
	}
	want := secretEnv{Refs: map[string]string{}}
	for _, key := range []string{
		"APP_ENV", "NATS_USER", "NATS_PASSWORD", "NATS_RPC_USER", "NATS_RPC_PASSWORD",
		"GITHUB_APP_ID", "GITHUB_APP_INSTALLATION_ID", "GITHUB_APP_PRIVATE_KEY",
		"GHCR_USERNAME", "GHCR_TOKEN",
		"DEPLOY_NATS_SIGNING_SEED", "DEPLOY_NATS_SYS_JWT", "DEPLOY_NATS_SYS_NKEY_SEED",
	} {
		want.Refs[key] = "deployer-env/" + key
	}
	if got := secretEnvOf(manifest); !reflect.DeepEqual(got, want) {
		t.Fatalf("deployer secret env:\n got %+v\nwant %+v", got, want)
	}
}
