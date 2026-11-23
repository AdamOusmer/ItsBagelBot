// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package k8s

import (
	"fmt"
	"regexp"
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

func TestControlsGatewayUsesItsOwnDopplerSecrets(t *testing.T) {
	deployment := findDeployment(t, "controls-gateway.yaml", "controls-gateway")
	assert.Equal(t, map[string]string{
		"OVERLAY_COOKIE_KEY":    "controls-gateway-env/OVERLAY_COOKIE_KEY",
		"NATS_JWT":              "controls-gateway-env/NATS_RPC_JWT",
		"NATS_NKEY_SEED":        "controls-gateway-env/NATS_RPC_NKEY_SEED",
		"NEW_RELIC_LICENSE_KEY": "controls-gateway-env/NEW_RELIC_LICENSE_KEY",
	}, deployment.secretEnv().Refs)
}

type traefikRoute struct {
	Match       string `yaml:"match"`
	Middlewares []struct {
		Name string `yaml:"name"`
	} `yaml:"middlewares"`
	Observability struct {
		AccessLogs *bool `yaml:"accessLogs"`
	} `yaml:"observability"`
	Services []struct {
		NativeLB *bool `yaml:"nativeLB"`
	} `yaml:"services"`
}

type traefikObject struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Routes    []traefikRoute `yaml:"routes"`
		RateLimit struct {
			Average         int    `yaml:"average"`
			Period          string `yaml:"period"`
			Burst           int    `yaml:"burst"`
			SourceCriterion struct {
				RequestHeaderName string `yaml:"requestHeaderName"`
			} `yaml:"sourceCriterion"`
		} `yaml:"rateLimit"`
	} `yaml:"spec"`
}

var sessionPathPattern = regexp.MustCompile("PathRegexp\\(`([^`]+)`\\)")

func (r traefikRoute) middlewareNames() []string {
	var names []string
	for _, m := range r.Middlewares {
		names = append(names, m.Name)
	}
	return names
}

func (r traefikRoute) sessionPatterns() []*regexp.Regexp {
	var patterns []*regexp.Regexp
	for _, m := range sessionPathPattern.FindAllStringSubmatch(r.Match, -1) {
		patterns = append(patterns, regexp.MustCompile(m[1]))
	}
	return patterns
}

func overlayRoutes(t *testing.T) []traefikRoute {
	t.Helper()
	docs := decodeFile[traefikObject](t, "controls-gateway.yaml")
	i := slices.IndexFunc(docs, func(doc traefikObject) bool { return doc.Kind == "IngressRoute" })
	require.GreaterOrEqual(t, i, 0)
	require.Len(t, docs[i].Spec.Routes, 2)
	return docs[i].Spec.Routes
}

func TestOverlayRoutesLimitSessionPathsAndStripClientIP(t *testing.T) {
	routes := overlayRoutes(t)
	for _, route := range routes {
		require.NotNil(t, route.Observability.AccessLogs)
		assert.False(t, *route.Observability.AccessLogs)
		require.NotEmpty(t, route.Services)
		for _, service := range route.Services {
			require.NotNil(t, service.NativeLB)
			assert.False(t, *service.NativeLB)
		}
	}
	session := slices.IndexFunc(routes, func(r traefikRoute) bool { return len(r.sessionPatterns()) > 0 })
	require.GreaterOrEqual(t, session, 0)
	catchAll := routes[1-session]
	assert.Equal(t, []string{"overlay-session-ratelimit", "strip-client-ip"}, routes[session].middlewareNames())
	assert.Equal(t, []string{"strip-client-ip"}, catchAll.middlewareNames())
	assert.Empty(t, catchAll.sessionPatterns())

	patterns := routes[session].sessionPatterns()
	covered := map[string]bool{
		"/tts/session": true, "/tts/session/": true, "//tts//session//": true,
		"/o/session": true, "/o/session/": true, "//o/session": true,
		"/powered/config": true, "/powered/config/": true, "//powered//config": true,
		"/tts/sessions": false, "/o/config": false, "/powered/session": false,
	}
	for path, want := range covered {
		got := slices.ContainsFunc(patterns, func(p *regexp.Regexp) bool { return p.MatchString(path) })
		assert.Equal(t, want, got, path)
	}
}

func TestOverlaySessionRateLimitValues(t *testing.T) {
	for _, doc := range decodeFile[traefikObject](t, "controls-gateway.yaml") {
		if doc.Kind == "Middleware" && doc.Metadata.Name == "overlay-session-ratelimit" {
			limit := doc.Spec.RateLimit
			assert.Equal(t, [4]any{10, "1m", 10, "CF-Connecting-IP"},
				[4]any{limit.Average, limit.Period, limit.Burst, limit.SourceCriterion.RequestHeaderName})
			return
		}
	}
	t.Fatal("overlay-session-ratelimit middleware missing")
}

type disruptionBudget struct {
	Kind     string     `yaml:"kind"`
	Metadata objectMeta `yaml:"metadata"`
	Spec     struct {
		MaxUnavailable int `yaml:"maxUnavailable"`
	} `yaml:"spec"`
}

func TestControlsGatewayRollsWithoutDroppingStreams(t *testing.T) {
	deployment := findDeployment(t, "controls-gateway.yaml", "controls-gateway")
	update := deployment.Spec.Strategy.RollingUpdate
	pod := deployment.Spec.Template.Spec
	require.Len(t, pod.Containers, 1)

	tests := []struct {
		name      string
		got, want any
	}{
		{"replicas", deployment.Spec.Replicas, 3},
		{"strategy", deployment.Spec.Strategy.Type, "RollingUpdate"},
		{"maxSurge", update.MaxSurge, 1},
		{"maxUnavailable", update.MaxUnavailable, 0},
		{"terminationGracePeriodSeconds", pod.TerminationGracePeriodSeconds, 45},
		{"preStop", pod.Containers[0].Lifecycle.PreStop.Exec.Command, []string{"sleep", "5"}},
		{"spread count", len(pod.TopologySpreadConstraints), 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { assert.Equal(t, tc.want, tc.got) })
	}

	budgets := decodeFile[disruptionBudget](t, "controls-gateway.yaml")
	i := slices.IndexFunc(budgets, func(b disruptionBudget) bool { return b.Kind == "PodDisruptionBudget" })
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, 1, budgets[i].Spec.MaxUnavailable)
}

func TestReloaderRunsOneRoller(t *testing.T) {
	ops := findDeployment(t, reloaderManifest, "reloader")

	assert.Equal(t, [2]any{1, "Recreate"}, [2]any{ops.Spec.Replicas, ops.Spec.Strategy.Type},
		"two rollers would stamp the same console rollout twice")
}

func TestSpreadIsHardWithoutMinDomains(t *testing.T) {
	tests := []struct{ file, name string }{
		{"admin.yaml", "admin"},
		{"console-admin.yaml", "console-admin"},
		{"console-dashboard.yaml", "console-dashboard"},
		{"notifications.yaml", "notifications"},
		{"overlays.yaml", "overlays"},
		{"transactions.yaml", "transactions"},
		{"twitch-ingress.yaml", "twitch-ingress"},
		{"controls-gateway.yaml", "controls-gateway"},
		{"outgress.yaml", "outgress"},
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

func TestConsoleDeploymentContract(t *testing.T) {
	for _, name := range []string{"console-admin", "console-dashboard"} {
		console := findDeployment(t, name+".yaml", name)
		assert.GreaterOrEqual(t, console.Spec.Replicas, 2, "%s needs a second replica to keep serving while one pod restarts", name)
		assert.Contains(t, console.Spec.Template.Spec.Containers[0].Env, envVar{Name: "NEW_RELIC_ERROR_COLLECTOR_EXPECTED_ERROR_CODES", Value: "400-499"},
			"%s must keep client 4xx out of the error rate and 5xx in it", name)
		assert.Contains(t, console.Spec.Template.Spec.Containers[0].Env, envVar{Name: "NEW_RELIC_IGNORING_RULES", Value: "^/healthz$,^/readyz$"},
			"%s must keep kubelet probes from spending the trace sampling budget", name)
	}
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

func TestNoProbeRunsAReleaseRPC(t *testing.T) {
	for filename, workloads := range allWorkloads(t) {
		for _, w := range workloads {
			for _, command := range w.probeCommands() {
				assert.False(t, slices.Contains(command, "pid") || slices.Contains(command, "rpc"),
					"%s/%s probes with %v; each run joins the node under a random name and leaves an atom that is never freed",
					filename, w.Metadata.Name, command)
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
