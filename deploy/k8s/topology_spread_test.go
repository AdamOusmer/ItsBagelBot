// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"
)

type workloadManifest struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Replicas int `yaml:"replicas"`
		Strategy struct {
			Type string `yaml:"type"`
		} `yaml:"strategy"`
		Template struct {
			Spec struct {
				TopologySpreadConstraints []struct {
					MinDomains        int      `yaml:"minDomains"`
					TopologyKey       string   `yaml:"topologyKey"`
					WhenUnsatisfiable string   `yaml:"whenUnsatisfiable"`
					MatchLabelKeys    []string `yaml:"matchLabelKeys"`
				} `yaml:"topologySpreadConstraints"`
				Affinity struct {
					NodeAffinity struct {
						Required struct {
							NodeSelectorTerms []struct {
								MatchExpressions []struct {
									Key      string   `yaml:"key"`
									Operator string   `yaml:"operator"`
									Values   []string `yaml:"values"`
								} `yaml:"matchExpressions"`
							} `yaml:"nodeSelectorTerms"`
						} `yaml:"requiredDuringSchedulingIgnoredDuringExecution"`
					} `yaml:"nodeAffinity"`
				} `yaml:"affinity"`
			} `yaml:"spec"`
		} `yaml:"template"`
		VolumeClaimTemplates []struct {
			Metadata struct {
				Name string `yaml:"name"`
			} `yaml:"metadata"`
		} `yaml:"volumeClaimTemplates"`
	} `yaml:"spec"`
}

func loadDeployment(t *testing.T, filename, name string) workloadManifest {
	t.Helper()

	f, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	for {
		var manifest workloadManifest
		if err := decoder.Decode(&manifest); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatal(err)
		}
		if manifest.Kind == "Deployment" && manifest.Metadata.Name == name {
			return manifest
		}
	}

	t.Fatalf("%s Deployment is missing from %s", name, filename)
	return workloadManifest{}
}

func hostnameSpread(t *testing.T, manifest workloadManifest) struct {
	MinDomains        int      `yaml:"minDomains"`
	TopologyKey       string   `yaml:"topologyKey"`
	WhenUnsatisfiable string   `yaml:"whenUnsatisfiable"`
	MatchLabelKeys    []string `yaml:"matchLabelKeys"`
} {
	t.Helper()
	for _, constraint := range manifest.Spec.Template.Spec.TopologySpreadConstraints {
		if constraint.TopologyKey == "kubernetes.io/hostname" {
			return constraint
		}
	}
	t.Fatalf("%s has no hostname topology spread constraint", manifest.Metadata.Name)
	return struct {
		MinDomains        int      `yaml:"minDomains"`
		TopologyKey       string   `yaml:"topologyKey"`
		WhenUnsatisfiable string   `yaml:"whenUnsatisfiable"`
		MatchLabelKeys    []string `yaml:"matchLabelKeys"`
	}{}
}

func TestSpreadIsHardWithoutMinDomains(t *testing.T) {
	for _, tt := range []struct{ file, name string }{
		{"console-admin.yaml", "console-admin"},
		{"console-dashboard.yaml", "console-dashboard"},
		{"notifications.yaml", "notifications"},
		{"transactions.yaml", "transactions"},
		{"twitch-ingress.yaml", "twitch-ingress"},
		{"outgress.yaml", "outgress"},
		{"discord.yaml", "discord-engine"},
		{"discord.yaml", "discord-outgress"},
		{"sesame.yaml", "sesame"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			constraint := hostnameSpread(t, loadDeployment(t, tt.file, tt.name))
			if constraint.WhenUnsatisfiable != "DoNotSchedule" {
				t.Errorf("hostname spread is %q; must be DoNotSchedule to enforce even placement (ScheduleAnyway piled all replicas onto one node on 2026-08-05)",
					constraint.WhenUnsatisfiable)
			}
			if !slices.Contains(constraint.MatchLabelKeys, "pod-template-hash") {
				t.Error("hostname spread must be scoped to the incoming ReplicaSet via matchLabelKeys")
			}
			if constraint.MinDomains != 0 {
				t.Errorf("minDomains = %d; do NOT set minDomains — it withholds ALL replicas when it exceeds the eligible domain count (see 2026-07-27 incident)",
					constraint.MinDomains)
			}
		})
	}
}

func excludesWorkerPool(key, operator string, values []string) bool {
	return key == "role" &&
		operator == "NotIn" &&
		slices.Contains(values, "worker")
}

func TestNoWorkloadSelectsNodesByHostname(t *testing.T) {
	for _, located := range loadDirectoryManifests(t) {
		for _, selector := range hostnameNodeSelectors(located.workloadManifest) {
			t.Errorf("%s/%s selects nodes by hostname (%s); select on role instead",
				located.filename, located.Metadata.Name, selector)
		}
	}
}

type locatedManifest struct {
	workloadManifest
	filename string
}

func loadDirectoryManifests(t *testing.T) []locatedManifest {
	t.Helper()

	var located []locatedManifest
	for _, filename := range manifestFilenames(t) {
		f, err := os.Open(filename)
		if err != nil {
			t.Fatal(err)
		}
		for _, manifest := range decodeManifests(t, f) {
			located = append(located, locatedManifest{workloadManifest: manifest, filename: filename})
		}
		f.Close()
	}
	return located
}

func manifestFilenames(t *testing.T) []string {
	t.Helper()

	filenames, err := filepath.Glob("*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(filenames) == 0 {
		t.Fatal("no manifests found; the glob is wrong")
	}
	return filenames
}

var pinningIsWorthIt = map[string]string{}

func TestOnlyDocumentedStatefulSetsPinAVolume(t *testing.T) {
	for _, located := range loadDirectoryManifests(t) {
		if located.Kind != "StatefulSet" || len(located.Spec.VolumeClaimTemplates) == 0 {
			continue
		}
		if _, ok := pinningIsWorthIt[located.Metadata.Name]; ok {
			continue
		}
		for _, vct := range located.Spec.VolumeClaimTemplates {
			t.Errorf("%s/%s claims volume %q but is not in pinningIsWorthIt; a local-path PVC strands the pod when its node dies, so either justify it there or use an emptyDir and let replication rebuild the member",
				located.filename, located.Metadata.Name, vct.Metadata.Name)
		}
	}
}

func decodeManifests(t *testing.T, r io.Reader) []workloadManifest {
	t.Helper()

	var manifests []workloadManifest
	decoder := yaml.NewDecoder(r)
	for {
		var manifest workloadManifest
		if err := decoder.Decode(&manifest); err != nil {
			if err != io.EOF {
				t.Fatal(err)
			}
			return manifests
		}
		manifests = append(manifests, manifest)
	}
}

func hostnameNodeSelectors(manifest workloadManifest) []string {
	var selectors []string
	for _, term := range manifest.Spec.Template.Spec.Affinity.NodeAffinity.Required.NodeSelectorTerms {
		for _, expression := range term.MatchExpressions {
			if expression.Key == "kubernetes.io/hostname" {
				selectors = append(selectors, fmt.Sprintf("%s %v", expression.Operator, expression.Values))
			}
		}
	}
	return selectors
}

func TestConsoleAdminExplicitlyExcludesWorkerPool(t *testing.T) {
	admin := loadDeployment(t, "console-admin.yaml", "console-admin")
	terms := admin.Spec.Template.Spec.Affinity.NodeAffinity.Required.NodeSelectorTerms

	for _, term := range terms {
		for _, expression := range term.MatchExpressions {
			if excludesWorkerPool(expression.Key, expression.Operator, expression.Values) {
				return
			}
		}
	}

	t.Fatal("console-admin must explicitly exclude the worker pool")
}

func TestDeployerRunsOneEngine(t *testing.T) {
	type rollout struct {
		Replicas int
		Strategy string
	}
	deployer := loadDeployment(t, deployerManifest, "deployer")
	got := rollout{Replicas: deployer.Spec.Replicas, Strategy: deployer.Spec.Strategy.Type}
	if want := (rollout{Replicas: 1, Strategy: "Recreate"}); got != want {
		t.Fatalf("deployer rollout = %+v, want %+v: a second engine would drive the same run", got, want)
	}
}
