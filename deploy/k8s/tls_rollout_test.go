// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const restartOnTLSSecret = "itsbagelbot.com/restart-on-tls-secret"

const (
	shutdownTimeoutSeconds = 30
	preStopSecondsMin      = 10
)

type rolloutDeployment struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name        string            `yaml:"name"`
		Annotations map[string]string `yaml:"annotations"`
	} `yaml:"metadata"`
	Spec struct {
		Strategy struct {
			Type          string `yaml:"type"`
			RollingUpdate struct {
				MaxSurge       int `yaml:"maxSurge"`
				MaxUnavailable int `yaml:"maxUnavailable"`
			} `yaml:"rollingUpdate"`
		} `yaml:"strategy"`
		Template struct {
			Spec struct {
				TerminationGracePeriodSeconds int `yaml:"terminationGracePeriodSeconds"`
				Containers                    []struct {
					Lifecycle struct {
						PreStop struct {
							Sleep struct {
								Seconds int `yaml:"seconds"`
							} `yaml:"sleep"`
						} `yaml:"preStop"`
					} `yaml:"lifecycle"`
					ReadinessProbe map[string]any `yaml:"readinessProbe"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
	} `yaml:"spec"`
}

func TestTLSOptInDeploymentsRollWithoutDroppingTraffic(t *testing.T) {
	opted := 0
	for _, filename := range manifestFilenames(t) {
		for _, dep := range decodeFile[rolloutDeployment](t, filename) {
			if dep.Kind != "Deployment" || dep.Metadata.Annotations[restartOnTLSSecret] == "" {
				continue
			}
			opted++
			t.Run(dep.Metadata.Name, func(t *testing.T) { assertZeroDowntimeRollout(t, dep) })
		}
	}
	assert.Equal(t, 2, opted, "console-admin and console-dashboard read their TLS pair only at startup")
}

func assertZeroDowntimeRollout(t *testing.T, dep rolloutDeployment) {
	t.Helper()
	update := dep.Spec.Strategy
	assert.Equal(t, "RollingUpdate", update.Type)
	assert.Zero(t, update.RollingUpdate.MaxUnavailable, "an old pod may leave only after its replacement is ready")
	assert.Equal(t, 1, update.RollingUpdate.MaxSurge)

	pod := dep.Spec.Template.Spec
	require.NotEmpty(t, pod.Containers)
	app := pod.Containers[0]
	assert.NotEmpty(t, app.ReadinessProbe, "readiness gates traffic to the replacement")
	assert.GreaterOrEqual(t, app.Lifecycle.PreStop.Sleep.Seconds, preStopSecondsMin,
		"endpoint removal must propagate before SIGTERM closes idle connections")
	assert.GreaterOrEqual(t, pod.TerminationGracePeriodSeconds, app.Lifecycle.PreStop.Sleep.Seconds+shutdownTimeoutSeconds,
		"grace must cover preStop plus the server drain")
}

func TestTLSOptInNamesOnlyIssuedSecrets(t *testing.T) {
	issued := readText(t, "../app/certificates.yaml")
	for _, filename := range manifestFilenames(t) {
		for _, dep := range decodeFile[rolloutDeployment](t, filename) {
			for _, name := range watchedSecretNames(dep) {
				assert.Contains(t, issued, "secretName: "+name, "%s watches %s, which no app Certificate issues", dep.Metadata.Name, name)
			}
		}
	}
}

func watchedSecretNames(dep rolloutDeployment) []string {
	var out []string
	for name := range strings.SplitSeq(dep.Metadata.Annotations[restartOnTLSSecret], ",") {
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}
