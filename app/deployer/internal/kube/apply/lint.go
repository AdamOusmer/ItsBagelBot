// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package apply

import (
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"

	"ItsBagelBot/app/deployer/internal/ports"
)

const hostnameKey = "kubernetes.io/hostname"

// strategy locates a workload kind's rolling-update block and the maxSurge
// the API server defaults when the manifest leaves it out.
type strategy struct {
	path         []string
	defaultSurge string
}

var strategies = map[schema.GroupKind]strategy{
	{Group: "apps", Kind: "Deployment"}: {path: []string{"spec", "strategy"}, defaultSurge: "25%"},
	{Group: "apps", Kind: "DaemonSet"}:  {path: []string{"spec", "updateStrategy"}, defaultSurge: "0"},
}

// lint refuses one-pod-per-node workloads that surge. The fleet has exactly as
// many application nodes as such a workload has replicas and no spare node to
// surge onto (see the maxSurge note in deploy/k8s/discord.yaml), so the surge
// pod stays Pending and the rollout never progresses. That is why
// notifications rolls at maxSurge 0 / maxUnavailable 1; a manifest edit that
// loses it is caught here, before anything is applied, instead of by the 8
// minute rollout timeout. A DoNotSchedule hostname spread
// scoped with matchLabelKeys [pod-template-hash] counts only the incoming
// ReplicaSet's pods, so its surge pod may share a host with an old one: that
// is how console-admin rolls at maxSurge 1 / maxUnavailable 0 and stays
// allowed. Required podAntiAffinity matches old pods too, so it gets no pass.
func lint(objs ports.Objects) []ports.LintFinding {
	var out []ports.LintFinding
	for _, o := range objs {
		st, ok := strategies[groupKind(o)]
		if !ok {
			continue
		}
		if f, bad := lintOne(o, st); bad {
			out = append(out, f)
		}
	}
	return out
}

func lintOne(o *unstructured.Unstructured, st strategy) (ports.LintFinding, bool) {
	why := onePerNode(o)
	if why == "" {
		return ports.LintFinding{}, false
	}
	surge, ok := surgeOf(o, st)
	if !ok {
		return ports.LintFinding{}, false
	}
	return ports.LintFinding{
		Object: refOf(o),
		Reason: fmt.Sprintf("%s with maxSurge %s: the surge pod has no free node and the rollout deadlocks; "+
			"use maxSurge 0 / maxUnavailable 1, or scope a DoNotSchedule spread with matchLabelKeys [pod-template-hash]",
			why, surge.String()),
	}, true
}

// onePerNode names the constraint that pins o to one pod per node, "" if none.
func onePerNode(o *unstructured.Unstructured) string {
	podSpec := []string{"spec", "template", "spec"}
	anti, _, _ := unstructured.NestedSlice(o.Object, slices.Concat(podSpec,
		[]string{"affinity", "podAntiAffinity", "requiredDuringSchedulingIgnoredDuringExecution"})...)
	if anyMap(anti, hostnameTerm) {
		return "required podAntiAffinity on " + hostnameKey
	}
	spread, _, _ := unstructured.NestedSlice(o.Object, slices.Concat(podSpec, []string{"topologySpreadConstraints"})...)
	if anyMap(spread, unscopedHostSpread) {
		return "DoNotSchedule topology spread on " + hostnameKey
	}
	return ""
}

func hostnameTerm(m map[string]any) bool { return m["topologyKey"] == hostnameKey }

func unscopedHostSpread(m map[string]any) bool {
	if !hostnameTerm(m) || m["whenUnsatisfiable"] != "DoNotSchedule" {
		return false
	}
	keys, _ := m["matchLabelKeys"].([]any)
	return !slices.Contains(keys, any("pod-template-hash"))
}

func anyMap(items []any, pred func(map[string]any) bool) bool {
	for _, it := range items {
		if m, ok := it.(map[string]any); ok && pred(m) {
			return true
		}
	}
	return false
}

// surgeOf returns the effective maxSurge and whether it is above zero.
// Recreate and OnDelete never surge; an unparsable value counts as surging so
// the lint fails closed.
func surgeOf(o *unstructured.Unstructured, st strategy) (intstr.IntOrString, bool) {
	typ, _, _ := unstructured.NestedString(o.Object, slices.Concat(st.path, []string{"type"})...)
	if typ == "Recreate" || typ == "OnDelete" {
		return intstr.FromInt32(0), false
	}
	v, found, _ := unstructured.NestedFieldNoCopy(o.Object, slices.Concat(st.path, []string{"rollingUpdate", "maxSurge"})...)
	if !found {
		v = st.defaultSurge
	}
	surge := intstr.Parse(fmt.Sprint(v))
	n, err := intstr.GetScaledValueFromIntOrPercent(&surge, 100, true)
	return surge, err != nil || n > 0
}
