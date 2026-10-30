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
						Required []antiTerm `yaml:"requiredDuringSchedulingIgnoredDuringExecution"`
					} `yaml:"podAntiAffinity"`
				} `yaml:"affinity"`
				Spread []spreadRule `yaml:"topologySpreadConstraints"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

type antiTerm struct {
	TopologyKey string `yaml:"topologyKey"`
}

type spreadRule struct {
	TopologyKey       string   `yaml:"topologyKey"`
	WhenUnsatisfiable string   `yaml:"whenUnsatisfiable"`
	MatchLabelKeys    []string `yaml:"matchLabelKeys"`
}

func (a antiTerm) pinsHost() bool { return a.TopologyKey == hostnameKey }

func (s spreadRule) pinsHost() bool {
	if s.TopologyKey != hostnameKey {
		return false
	}
	if s.WhenUnsatisfiable != "DoNotSchedule" {
		return false
	}
	return !slices.Contains(s.MatchLabelKeys, "pod-template-hash")
}

func (m rolloutManifest) onePerNode() bool {
	pod := m.Spec.Template.Spec
	return slices.ContainsFunc(pod.Affinity.PodAntiAffinity.Required, antiTerm.pinsHost) ||
		slices.ContainsFunc(pod.Spread, spreadRule.pinsHost)
}

func (m rolloutManifest) deadlocks() bool {
	if m.Kind != "Deployment" && m.Kind != "DaemonSet" {
		return false
	}
	return m.onePerNode() && m.nodesForRoll() > eligibleHosts
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
			if m.deadlocks() {
				deadlocks = append(deadlocks, fmt.Sprintf("%s %s/%s needs %d nodes", m.Kind, m.Metadata.Namespace, m.Metadata.Name, m.nodesForRoll()))
			}
		}
	}

	assert.Empty(t, deadlocks, "the surge pod has no free node and the rollout deadlocks; use maxSurge 0 / maxUnavailable 1, "+
		"keep replicas + maxSurge <= %d, or scope a DoNotSchedule spread with matchLabelKeys [pod-template-hash]", eligibleHosts)
}
