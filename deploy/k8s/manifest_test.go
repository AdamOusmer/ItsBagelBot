// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type objectMeta struct {
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

type workload struct {
	Kind     string     `yaml:"kind"`
	Metadata objectMeta `yaml:"metadata"`
	Spec     struct {
		Replicas int `yaml:"replicas"`
		Strategy struct {
			Type string `yaml:"type"`
		} `yaml:"strategy"`
		Template struct {
			Spec podSpec `yaml:"spec"`
		} `yaml:"template"`
		VolumeClaimTemplates []struct {
			Metadata objectMeta `yaml:"metadata"`
		} `yaml:"volumeClaimTemplates"`
	} `yaml:"spec"`
}

type podSpec struct {
	Containers                []container        `yaml:"containers"`
	TopologySpreadConstraints []spreadConstraint `yaml:"topologySpreadConstraints"`
	Affinity                  struct {
		NodeAffinity struct {
			Required struct {
				NodeSelectorTerms []struct {
					MatchExpressions []nodeExpression `yaml:"matchExpressions"`
				} `yaml:"nodeSelectorTerms"`
			} `yaml:"requiredDuringSchedulingIgnoredDuringExecution"`
		} `yaml:"nodeAffinity"`
	} `yaml:"affinity"`
}

type container struct {
	Env     []envVar `yaml:"env"`
	EnvFrom []any    `yaml:"envFrom"`
}

type envVar struct {
	Name      string `yaml:"name"`
	ValueFrom struct {
		SecretKeyRef struct {
			Name string `yaml:"name"`
			Key  string `yaml:"key"`
		} `yaml:"secretKeyRef"`
	} `yaml:"valueFrom"`
}

type spreadConstraint struct {
	MinDomains        int      `yaml:"minDomains"`
	TopologyKey       string   `yaml:"topologyKey"`
	WhenUnsatisfiable string   `yaml:"whenUnsatisfiable"`
	MatchLabelKeys    []string `yaml:"matchLabelKeys"`
}

type nodeExpression struct {
	Key      string   `yaml:"key"`
	Operator string   `yaml:"operator"`
	Values   []string `yaml:"values"`
}

func (w workload) nodeExpressions() []nodeExpression {
	var out []nodeExpression
	for _, term := range w.Spec.Template.Spec.Affinity.NodeAffinity.Required.NodeSelectorTerms {
		out = append(out, term.MatchExpressions...)
	}
	return out
}

func (w workload) envNames() []string {
	out := []string{}
	for _, c := range w.Spec.Template.Spec.Containers {
		for _, e := range c.Env {
			out = append(out, e.Name)
		}
	}
	return out
}

type secretEnv struct {
	Refs    map[string]string
	EnvFrom int
}

func (w workload) secretEnv() secretEnv {
	out := secretEnv{Refs: map[string]string{}}
	for _, c := range w.Spec.Template.Spec.Containers {
		out.EnvFrom += len(c.EnvFrom)
		for _, e := range c.Env {
			if ref := e.ValueFrom.SecretKeyRef; ref.Key != "" {
				out.Refs[e.Name] = ref.Name + "/" + ref.Key
			}
		}
	}
	return out
}

func (w workload) envFromSecret(secret string) bool {
	for _, c := range w.Spec.Template.Spec.Containers {
		for _, source := range c.EnvFrom {
			entry, _ := source.(map[string]any)
			ref, _ := entry["secretRef"].(map[string]any)
			if ref["name"] == secret {
				return true
			}
		}
	}
	return false
}

func decodeFile[T any](t *testing.T, path string) []T {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()

	var out []T
	decoder := yaml.NewDecoder(f)
	for {
		var manifest T
		err := decoder.Decode(&manifest)
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		out = append(out, manifest)
	}
}

func findDeployment(t *testing.T, filename, name string) workload {
	t.Helper()
	for _, manifest := range decodeFile[workload](t, filename) {
		if manifest.Kind == "Deployment" && manifest.Metadata.Name == name {
			return manifest
		}
	}
	require.Failf(t, "missing Deployment", "%s Deployment is missing from %s", name, filename)
	return workload{}
}

func manifestFilenames(t *testing.T) []string {
	t.Helper()
	filenames, err := filepath.Glob("*.yaml")
	require.NoError(t, err)
	require.NotEmpty(t, filenames, "no manifests found; the glob is wrong")
	return filenames
}

func allWorkloads(t *testing.T) map[string][]workload {
	t.Helper()
	out := map[string][]workload{}
	for _, filename := range manifestFilenames(t) {
		out[filename] = decodeFile[workload](t, filename)
	}
	return out
}

func readText(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(body)
}

func sorted(values ...string) []string {
	slices.Sort(values)
	return values
}
