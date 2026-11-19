// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"testing"
	"time"

	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sesameDeliveryManifest struct {
	Kind string `yaml:"kind"`
	Spec struct {
		Strategy struct {
			RollingUpdate struct {
				MaxSurge       string `yaml:"maxSurge"`
				MaxUnavailable int    `yaml:"maxUnavailable"`
			} `yaml:"rollingUpdate"`
		} `yaml:"strategy"`
		Template struct {
			Spec struct {
				Grace      int `yaml:"terminationGracePeriodSeconds"`
				Containers []struct {
					Env []struct{ Name, Value string } `yaml:"env"`
				} `yaml:"containers"`
			} `yaml:"spec"`
		} `yaml:"template"`
		MinReplicas int `yaml:"minReplicaCount"`
		Triggers    []struct {
			Type     string            `yaml:"type"`
			Metadata map[string]string `yaml:"metadata"`
		} `yaml:"triggers"`
	} `yaml:"spec"`
}

func TestSesameDeliveryRolloutAndShutdownBudget(t *testing.T) {
	manifest := sesameDeliveryResource(t, "Deployment")
	roll := manifest.Spec.Strategy.RollingUpdate
	assert.Equal(t, [2]any{"100%", 0}, [2]any{roll.MaxSurge, roll.MaxUnavailable})
	pod := manifest.Spec.Template.Spec
	drain, err := time.ParseDuration(sesameDeliveryEnv(t, manifest)["SESAME_DRAIN_TIMEOUT"])
	require.NoError(t, err)
	require.Positive(t, drain, "pin the handler drain explicitly")
	assert.Greater(t, time.Duration(pod.Grace)*time.Second, drain+30*time.Second+2*time.Second)
	for _, pin := range []struct{ file, code string }{
		{"../../pkg/bus/concurrent_subscriber.go", "subscriberDrainTimeout = 30 * time.Second"},
		{"../../pkg/bus/pull_consumer.go", "s.nc.FlushTimeout(2 * time.Second)"},
	} {
		assert.Contains(t, readText(t, pin.file), pin.code, "update the shutdown budget when the bus timeout changes")
	}
}

func TestSesameLagTracksActiveDurablesWithCompletionHeadroom(t *testing.T) {
	env := sesameDeliveryEnv(t, sesameDeliveryResource(t, "Deployment"))
	mode := env["NATS_HOT_LANE_MODE"]
	require.Contains(t, []string{"pull", "push"}, mode)
	t.Setenv("NATS_HOT_LANE_MODE", mode)
	want := map[string]string{}
	for _, spec := range []bus.StreamSpec{bus.TwitchIngressPremiumStream, bus.TwitchIngressStandardStream} {
		want[spec.Name] = bus.HotLaneConsumerName(env["SESAME_CONSUMER_NAME"], spec.Subjects[0])
	}
	got := map[string]string{}
	manifest := sesameDeliveryResource(t, "ScaledObject")
	for _, trigger := range manifest.Spec.Triggers {
		if trigger.Type != "nats-jetstream" {
			continue
		}
		got[trigger.Metadata["stream"]] = trigger.Metadata["consumer"]
		assert.Equal(t, [2]string{"5000", "10000"}, [2]string{trigger.Metadata["lagThreshold"], trigger.Metadata["activationLagThreshold"]})
		assert.Greater(t, manifest.Spec.MinReplicas*5000, 20000/2)
	}
	assert.Equal(t, want, got)
}

func sesameDeliveryEnv(t *testing.T, manifest sesameDeliveryManifest) map[string]string {
	t.Helper()
	containers := manifest.Spec.Template.Spec.Containers
	require.Len(t, containers, 1)
	values := make(map[string]string, len(containers[0].Env))
	for _, variable := range containers[0].Env {
		values[variable.Name] = variable.Value
	}
	return values
}

func sesameDeliveryResource(t *testing.T, kind string) sesameDeliveryManifest {
	t.Helper()
	for _, manifest := range decodeFile[sesameDeliveryManifest](t, "sesame.yaml") {
		if manifest.Kind == kind {
			return manifest
		}
	}
	t.Fatalf("missing Sesame %s", kind)
	return sesameDeliveryManifest{}
}
