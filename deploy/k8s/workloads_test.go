// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBotIdentityComesFromTheRightSecret(t *testing.T) {
	const botID = "TWITCH_BOT_USER_ID"
	tests := []struct {
		name      string
		file      string
		workload  string
		envFrom   string
		pinnedRef string
	}{
		{name: "TestUsersReadsBotIdentityFromDoppler", file: "users.yaml", workload: "users", envFrom: "users-env"},
		{name: "console-admin reads it from its own Doppler secret", file: "console-admin.yaml", workload: "console-admin", envFrom: "console-admin-env"},
		{name: "console-dashboard shares the outgress bot identity so sign-in cannot swap in a broadcaster grant",
			file: "console-dashboard.yaml", workload: "console-dashboard", pinnedRef: "outgress-env/" + botID},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deployment := findDeployment(t, tc.file, tc.workload)

			if tc.pinnedRef != "" {
				assert.Equal(t, tc.pinnedRef, deployment.secretEnv().Refs[botID])
				return
			}
			assert.NotContains(t, deployment.envNames(), botID, "an inline value overrides the %s Doppler value", tc.envFrom)
			assert.True(t, deployment.envFromSecret(tc.envFrom), "%s lost envFrom %s, its only %s source", tc.workload, tc.envFrom, botID)
		})
	}
}

func TestDiscordEnvContracts(t *testing.T) {
	tests := []struct {
		name        string
		deployments []string
		env         string
		wantPresent bool
	}{
		{name: "TestDiscordManifestsCarryNoDeadDataSwitch", deployments: []string{"discord-engine", "discord-outgress", "discord-ingress"},
			env: "DISCORD_DATA_ENABLED"},
		{name: "TestDiscordDataPrefixSurvives", deployments: []string{"discord-engine", "discord-outgress"},
			env: "NATS_DISCORD_DATA_RPC_PREFIX", wantPresent: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, name := range tc.deployments {
				assert.Equal(t, tc.wantPresent, slices.Contains(findDeployment(t, "discord.yaml", name).envNames(), tc.env),
					"%s %s presence", name, tc.env)
			}
		})
	}
}

func TestClusterOpsSecretsAreWiredOneByOne(t *testing.T) {
	want := secretEnv{Refs: map[string]string{
		"APP_ENV":            "clusterops-env/APP_ENV",
		"NATS_SIGNING_SEED":  "clusterops-env/DEPLOY_NATS_SIGNING_SEED",
		"NATS_SYS_JWT":       "clusterops-env/DEPLOY_NATS_SYS_JWT",
		"NATS_SYS_NKEY_SEED": "clusterops-env/DEPLOY_NATS_SYS_NKEY_SEED",
	}}

	assert.Equal(t, want, findDeployment(t, clusterOpsManifest, "clusterops").secretEnv())
}

func TestClusterOpsRunsOneRoller(t *testing.T) {
	ops := findDeployment(t, clusterOpsManifest, "clusterops")

	assert.Equal(t, [2]any{1, "Recreate"}, [2]any{ops.Spec.Replicas, ops.Spec.Strategy.Type},
		"two rollers would stamp the same console rollout twice")
}

func TestSpreadIsHardWithoutMinDomains(t *testing.T) {
	tests := []struct{ file, name string }{
		{"console-admin.yaml", "console-admin"},
		{"console-dashboard.yaml", "console-dashboard"},
		{"notifications.yaml", "notifications"},
		{"transactions.yaml", "transactions"},
		{"twitch-ingress.yaml", "twitch-ingress"},
		{"outgress.yaml", "outgress"},
		{"discord.yaml", "discord-ingress"},
		{"discord.yaml", "discord-engine"},
		{"discord.yaml", "discord-outgress"},
		{"sesame.yaml", "sesame"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			constraints := findDeployment(t, tc.file, tc.name).Spec.Template.Spec.TopologySpreadConstraints
			i := slices.IndexFunc(constraints, func(c spreadConstraint) bool { return c.TopologyKey == "kubernetes.io/hostname" })
			require.GreaterOrEqual(t, i, 0, "no hostname topology spread constraint")

			assert.Equal(t, "DoNotSchedule", constraints[i].WhenUnsatisfiable,
				"ScheduleAnyway piled all replicas onto one node on 2026-08-05")
			assert.Contains(t, constraints[i].MatchLabelKeys, "pod-template-hash",
				"hostname spread must be scoped to the incoming ReplicaSet")
			assert.Zero(t, constraints[i].MinDomains,
				"minDomains withholds ALL replicas when it exceeds the eligible domain count (2026-07-27 incident)")
		})
	}
}

func TestConsoleAdminExplicitlyExcludesWorkerPool(t *testing.T) {
	admin := findDeployment(t, "console-admin.yaml", "console-admin")

	excludes := slices.ContainsFunc(admin.nodeExpressions(), func(e nodeExpression) bool {
		return e.Key == "role" && e.Operator == "NotIn" && slices.Contains(e.Values, "worker")
	})

	assert.True(t, excludes, "console-admin must explicitly exclude the worker pool")
}

func TestNoWorkloadSelectsNodesByHostname(t *testing.T) {
	for filename, workloads := range allWorkloads(t) {
		for _, w := range workloads {
			for _, e := range w.nodeExpressions() {
				assert.NotEqual(t, "kubernetes.io/hostname", e.Key, "%s/%s selects nodes by hostname (%s %v); select on role instead",
					filename, w.Metadata.Name, e.Operator, e.Values)
			}
		}
	}
}

var pinningIsWorthIt = map[string]string{}

func TestOnlyDocumentedStatefulSetsPinAVolume(t *testing.T) {
	for filename, workloads := range allWorkloads(t) {
		for _, w := range workloads {
			if _, documented := pinningIsWorthIt[w.Metadata.Name]; documented || w.Kind != "StatefulSet" {
				continue
			}
			for _, vct := range w.Spec.VolumeClaimTemplates {
				assert.Fail(t, "undocumented pinned volume", fmt.Sprintf(
					"%s/%s claims volume %q but is not in pinningIsWorthIt; a local-path PVC strands the pod when its node dies, so either justify it there or use an emptyDir and let replication rebuild the member",
					filename, w.Metadata.Name, vct.Metadata.Name))
			}
		}
	}
}
