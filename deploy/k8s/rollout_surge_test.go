// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const hostnameKey = "kubernetes.io/hostname"

// Application nodes a one-per-node workload can land on; if the fleet shrinks below this, a deadlocking roll passes.
const eligibleHosts = 3

type rollStrategy struct {
	Type          string `yaml:"type"`
	RollingUpdate struct {
		MaxSurge any `yaml:"maxSurge"`
	} `yaml:"rollingUpdate"`
}

type rolloutManifest struct {
	Kind     string     `yaml:"kind"`
	Metadata objectMeta `yaml:"metadata"`
	Spec     struct {
		Replicas       *int         `yaml:"replicas"`
		Strategy       rollStrategy `yaml:"strategy"`
		UpdateStrategy rollStrategy `yaml:"updateStrategy"`
		Template       struct {
			Spec struct {
				Affinity struct {
					PodAntiAffinity struct {
						Required []struct {
							TopologyKey string `yaml:"topologyKey"`
						} `yaml:"requiredDuringSchedulingIgnoredDuringExecution"`
					} `yaml:"podAntiAffinity"`
				} `yaml:"affinity"`
				Spread []struct {
					TopologyKey       string   `yaml:"topologyKey"`
					WhenUnsatisfiable string   `yaml:"whenUnsatisfiable"`
					MatchLabelKeys    []string `yaml:"matchLabelKeys"`
				} `yaml:"topologySpreadConstraints"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

func (m rolloutManifest) onePerNode() bool {
	pod := m.Spec.Template.Spec
	for _, term := range pod.Affinity.PodAntiAffinity.Required {
		if term.TopologyKey == hostnameKey {
			return true
		}
	}
	for _, s := range pod.Spread {
		if s.TopologyKey == hostnameKey && s.WhenUnsatisfiable == "DoNotSchedule" && !slices.Contains(s.MatchLabelKeys, "pod-template-hash") {
			return true
		}
	}
	return false
}

func (m rolloutManifest) nodesForRoll() int {
	replicas, st, surge := 1, m.Spec.Strategy, "25%"
	if m.Spec.Replicas != nil {
		replicas = *m.Spec.Replicas
	}
	if m.Kind == "DaemonSet" {
		replicas, st, surge = eligibleHosts, m.Spec.UpdateStrategy, "0"
	}
	if st.Type == "Recreate" || st.Type == "OnDelete" {
		return replicas
	}
	if st.RollingUpdate.MaxSurge != nil {
		surge = fmt.Sprint(st.RollingUpdate.MaxSurge)
	}
	parsed := intstr.Parse(surge)
	extra, err := intstr.GetScaledValueFromIntOrPercent(&parsed, replicas, true)
	if err != nil {
		return replicas + eligibleHosts
	}
	return replicas + extra
}

func TestOnePerNodeRollsFitTheEligibleHosts(t *testing.T) {
	var deadlocks []string
	for _, filename := range manifestFilenames(t) {
		for _, m := range decodeFile[rolloutManifest](t, filename) {
			if (m.Kind == "Deployment" || m.Kind == "DaemonSet") && m.onePerNode() && m.nodesForRoll() > eligibleHosts {
				deadlocks = append(deadlocks, fmt.Sprintf("%s %s/%s needs %d nodes", m.Kind, m.Metadata.Namespace, m.Metadata.Name, m.nodesForRoll()))
			}
		}
	}

	assert.Empty(t, deadlocks, "the surge pod has no free node and the rollout deadlocks; use maxSurge 0 / maxUnavailable 1, "+
		"keep replicas + maxSurge <= %d, or scope a DoNotSchedule spread with matchLabelKeys [pod-template-hash]", eligibleHosts)
}
