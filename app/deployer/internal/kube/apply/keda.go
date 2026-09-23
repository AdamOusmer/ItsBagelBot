// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package apply

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"ItsBagelBot/app/deployer/internal/ports"
)

var (
	deploymentKind   = schema.GroupKind{Group: "apps", Kind: "Deployment"}
	scaledObjectKind = schema.GroupKind{Group: "keda.sh", Kind: "ScaledObject"}
)

type workloadKey struct {
	ns   ports.Namespace
	name string
}

// dropScaledReplicas returns objs with spec.replicas removed from every
// Deployment a KEDA ScaledObject targets, whether that ScaledObject is in objs
// or only live in the Deployment's namespace. The manifests keep replicas
// (sesame.yaml, outgress.yaml) as the floor for a fresh apply; re-applying it
// with force would snap a scaled-out Deployment back to that floor on every
// deploy until the HPA caught up. Callers' objects are never mutated: the
// stage reuses them for lint and verify.
func (a *Applier) dropScaledReplicas(ctx context.Context, objs ports.Objects) (ports.Objects, error) {
	targets, nss := scan(objs)
	for ns := range nss {
		if err := a.liveScaledTargets(ctx, ns, targets); err != nil {
			return nil, err
		}
	}
	out := make(ports.Objects, len(objs))
	for i, o := range objs {
		out[i] = withoutReplicas(o, targets)
	}
	return out, nil
}

func withoutReplicas(o *unstructured.Unstructured, targets map[workloadKey]bool) *unstructured.Unstructured {
	if groupKind(o) != deploymentKind {
		return o
	}
	if !targets[workloadKey{ports.Namespace(o.GetNamespace()), o.GetName()}] {
		return o
	}
	c := o.DeepCopy()
	unstructured.RemoveNestedField(c.Object, "spec", "replicas")
	return c
}

// liveScaledTargets adds the targets of ScaledObjects live in ns. A cluster
// without the KEDA CRD has none, which is an answer, not an error.
func (a *Applier) liveScaledTargets(ctx context.Context, ns ports.Namespace, into map[workloadKey]bool) error {
	m, err := a.mapper.RESTMapping(scaledObjectKind)
	if meta.IsNoMatchError(err) {
		return nil
	}
	if err != nil {
		return err
	}
	list, err := a.dyn.Resource(m.Resource).Namespace(string(ns)).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list ScaledObjects in %s: %w", ns, err)
	}
	for i := range list.Items {
		addTarget(into, &list.Items[i])
	}
	return nil
}

// scan collects the targets of the ScaledObjects in objs and the namespaces
// holding a Deployment (the only namespaces worth a live ScaledObject list).
func scan(objs ports.Objects) (map[workloadKey]bool, map[ports.Namespace]bool) {
	targets := map[workloadKey]bool{}
	nss := map[ports.Namespace]bool{}
	for _, o := range objs {
		switch groupKind(o) {
		case scaledObjectKind:
			addTarget(targets, o)
		case deploymentKind:
			nss[ports.Namespace(o.GetNamespace())] = true
		}
	}
	return targets, nss
}

// addTarget records so's scaleTargetRef. An empty kind means Deployment (the
// KEDA default); any other kind is not a Deployment this applier touches.
func addTarget(into map[workloadKey]bool, so *unstructured.Unstructured) {
	ref, _, _ := unstructured.NestedStringMap(so.Object, "spec", "scaleTargetRef")
	kind := ref["kind"]
	if kind != "" && kind != "Deployment" {
		return
	}
	into[workloadKey{ports.Namespace(so.GetNamespace()), ref["name"]}] = true
}
