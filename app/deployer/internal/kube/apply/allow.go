// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package apply

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

type scope int

const (
	scopeRefused scope = iota
	scopeNamespaced
	scopeCluster
)

// Must not grow DopplerSecret, Namespace, ServiceAccount, Secret or RBAC kinds.
var allowed = map[schema.GroupKind]scope{
	{Group: "apps", Kind: "Deployment"}:                 scopeNamespaced,
	{Group: "apps", Kind: "DaemonSet"}:                  scopeNamespaced,
	{Group: "batch", Kind: "CronJob"}:                   scopeNamespaced,
	{Group: "", Kind: "Service"}:                        scopeNamespaced,
	{Group: "", Kind: "ConfigMap"}:                      scopeNamespaced,
	{Group: "policy", Kind: "PodDisruptionBudget"}:      scopeNamespaced,
	{Group: "networking.k8s.io", Kind: "NetworkPolicy"}: scopeNamespaced,
	{Group: "traefik.io", Kind: "IngressRoute"}:         scopeNamespaced,
	{Group: "traefik.io", Kind: "Middleware"}:           scopeNamespaced,
	{Group: "keda.sh", Kind: "ScaledObject"}:            scopeNamespaced,
	{Group: "scheduling.k8s.io", Kind: "PriorityClass"}: scopeCluster,
}

var namespaces = map[ports.Namespace]bool{"app": true, "db": true, "messaging": true}

// Its ops Role grants patch on this one Deployment and nothing else there.
var self = ports.ObjectRef{Kind: "Deployment", Namespace: "ops", Name: "deployer"}

func Split(objs ports.Objects) (ports.Objects, []ports.ObjectRef) {
	var ok ports.Objects
	var out []ports.ObjectRef
	for _, o := range objs {
		if refusal(o) != "" {
			out = append(out, refOf(o))
			continue
		}
		ok = append(ok, o)
	}
	return ok, out
}

func refuse(objs ports.Objects) error {
	var lines []string
	for _, o := range objs {
		if why := refusal(o); why != "" {
			lines = append(lines, refString(refOf(o))+": "+why)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	f := ports.Failf(deploy.FailApplyRefused, "refused %d object(s): %s", len(lines), strings.Join(lines, "; "))
	f.LogTail = lines
	return fmt.Errorf("%w: %w", f, ports.ErrRefused)
}

func refusal(o *unstructured.Unstructured) string {
	gk := groupKind(o)
	switch allowed[gk] {
	case scopeRefused:
		return fmt.Sprintf("kind %s is not on the allowlist", gk)
	case scopeNamespaced:
		return namespaceRefusal(o)
	default:
		return ""
	}
}

func namespaceRefusal(o *unstructured.Unstructured) string {
	ns := ports.Namespace(o.GetNamespace())
	if namespaces[ns] || refOf(o) == self {
		return ""
	}
	return fmt.Sprintf("namespace %q is outside app, db, messaging", ns)
}

func groupKind(o *unstructured.Unstructured) schema.GroupKind {
	return o.GroupVersionKind().GroupKind()
}

func refOf(o *unstructured.Unstructured) ports.ObjectRef {
	return ports.ObjectRef{Kind: o.GetKind(), Namespace: ports.Namespace(o.GetNamespace()), Name: o.GetName()}
}

func refString(r ports.ObjectRef) string {
	if r.Namespace == "" {
		return r.Kind + " " + r.Name
	}
	return r.Kind + " " + string(r.Namespace) + "/" + r.Name
}
