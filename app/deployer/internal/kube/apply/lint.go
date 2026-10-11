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

// Application nodes a one-per-node workload can land on; if the fleet shrinks below this, a deadlocking roll passes lint.
const eligibleHosts = 3

type strategy struct {
	path         []string
	defaultSurge string
	replicas     func(o *unstructured.Unstructured) int64
}

var strategies = map[schema.GroupKind]strategy{
	{Group: "apps", Kind: "Deployment"}: {path: []string{"spec", "strategy"}, defaultSurge: "25%", replicas: specReplicas},
	{Group: "apps", Kind: "DaemonSet"}:  {path: []string{"spec", "updateStrategy"}, defaultSurge: "0", replicas: everyHost},
}

func specReplicas(o *unstructured.Unstructured) int64 {
	n, found, err := unstructured.NestedInt64(o.Object, "spec", "replicas")
	if !found || err != nil {
		return 1
	}
	return n
}

func everyHost(*unstructured.Unstructured) int64 { return eligibleHosts }

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
	replicas := st.replicas(o)
	surge, extra := surgeOf(o, st, replicas)
	if replicas+extra <= eligibleHosts {
		return ports.LintFinding{}, false
	}
	return ports.LintFinding{
		Object: refOf(o),
		Reason: fmt.Sprintf("%s with %d replicas and maxSurge %s: a roll needs %d nodes but %d are eligible, so the surge pod "+
			"has no free node and the rollout deadlocks; use maxSurge 0 / maxUnavailable 1, keep replicas + maxSurge <= %d, "+
			"or scope a DoNotSchedule spread with matchLabelKeys [pod-template-hash]",
			why, replicas, surge.String(), replicas+extra, eligibleHosts, eligibleHosts),
	}, true
}

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

func surgeOf(o *unstructured.Unstructured, st strategy, replicas int64) (intstr.IntOrString, int64) {
	typ, _, _ := unstructured.NestedString(o.Object, slices.Concat(st.path, []string{"type"})...)
	if typ == "Recreate" || typ == "OnDelete" {
		return intstr.FromInt32(0), 0
	}
	v, found, _ := unstructured.NestedFieldNoCopy(o.Object, slices.Concat(st.path, []string{"rollingUpdate", "maxSurge"})...)
	if !found {
		v = st.defaultSurge
	}
	surge := intstr.Parse(fmt.Sprint(v))
	n, err := intstr.GetScaledValueFromIntOrPercent(&surge, int(replicas), true)
	if err != nil {
		return surge, eligibleHosts
	}
	return surge, int64(n)
}
