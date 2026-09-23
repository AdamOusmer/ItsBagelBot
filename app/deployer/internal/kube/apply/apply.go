// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package apply implements ports.Applier: kustomize build from in-memory
// files, the manifest lint, and allowlisted server-side apply.
package apply

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

// FieldManager is the server-side apply field manager.
const FieldManager = "bagel-deployer"

// Applier implements ports.Applier.
type Applier struct {
	dyn    dynamic.Interface
	mapper meta.RESTMapper
}

var _ ports.Applier = (*Applier)(nil)

// New builds the dynamic client and REST mapper from rc.
func New(rc *rest.Config) (*Applier, error) {
	dyn, err := dynamic.NewForConfig(rc)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	dc, err := discovery.NewDiscoveryClientForConfig(rc)
	if err != nil {
		return nil, fmt.Errorf("discovery client: %w", err)
	}
	return newApplier(dyn, restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))), nil
}

func newApplier(dyn dynamic.Interface, mapper meta.RESTMapper) *Applier {
	return &Applier{dyn: dyn, mapper: mapper}
}

// Build renders spec with kustomize; see build.go.
func (a *Applier) Build(_ context.Context, spec ports.BuildSpec) (ports.Objects, error) {
	return build(spec)
}

// Lint runs the one-pod-per-node surge check; see lint.go.
func (a *Applier) Lint(objs ports.Objects) []ports.LintFinding { return lint(objs) }

// Apply server-side applies objs, cluster-scoped objects first, one object at
// a time, and stops at the first error with the objects applied so far. The
// allowlist is checked for the whole set before anything is sent, so a refused
// object never leaves the set half applied.
func (a *Applier) Apply(ctx context.Context, objs ports.Objects) (ports.ApplyResult, error) {
	if err := refuse(objs); err != nil {
		return ports.ApplyResult{}, err
	}
	// The process lives for weeks while KEDA and Traefik CRDs are installed
	// and upgraded under it; a mapper cached at boot answers "no match" for a
	// kind that appeared later until the pod restarts. One rediscovery per
	// Apply costs two requests against aggregated discovery.
	meta.MaybeResetRESTMapper(a.mapper)
	work, err := a.dropScaledReplicas(ctx, objs)
	if err != nil {
		return ports.ApplyResult{}, err
	}
	var res ports.ApplyResult
	for _, o := range staged(work) {
		changed, err := a.applyOne(ctx, o)
		if err != nil {
			return res, applyFailed(o, err)
		}
		res = record(res, refOf(o), changed)
	}
	return res, nil
}

// applyOne applies o and reports whether its resourceVersion moved. A no-op
// server-side apply keeps the resourceVersion, so the GET before is the only
// way to tell "applied" from "changed"; managedFields timestamps move only
// when this manager's field set changes, which misses value edits.
func (a *Applier) applyOne(ctx context.Context, o *unstructured.Unstructured) (bool, error) {
	ri, err := a.resource(o)
	if err != nil {
		return false, err
	}
	before, err := resourceVersion(ctx, ri, o)
	if err != nil {
		return false, err
	}
	after, err := ri.Apply(ctx, o.GetName(), o, metav1.ApplyOptions{FieldManager: FieldManager, Force: true})
	if err != nil {
		return false, err
	}
	return after.GetResourceVersion() != before, nil
}

func (a *Applier) resource(o *unstructured.Unstructured) (dynamic.ResourceInterface, error) {
	gvk := o.GroupVersionKind()
	m, err := a.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}
	if m.Scope.Name() != meta.RESTScopeNameNamespace {
		return a.dyn.Resource(m.Resource), nil
	}
	return a.dyn.Resource(m.Resource).Namespace(o.GetNamespace()), nil
}

// resourceVersion is the live object's resourceVersion, "" when it does not
// exist yet (so an apply that creates it counts as changed).
func resourceVersion(ctx context.Context, ri dynamic.ResourceInterface, o *unstructured.Unstructured) (string, error) {
	live, err := ri.Get(ctx, o.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return live.GetResourceVersion(), nil
}

func record(res ports.ApplyResult, ref ports.ObjectRef, changed bool) ports.ApplyResult {
	res.Applied = append(res.Applied, ref)
	if changed {
		res.Changed = append(res.Changed, ref)
	}
	return res
}

func applyFailed(o *unstructured.Unstructured, err error) error {
	f := ports.Failf(deploy.FailApplyFailed, "apply %s: %v", refString(refOf(o)), err)
	return fmt.Errorf("%w: %w", f, err)
}

// staged puts cluster-scoped objects (PriorityClasses) ahead of everything
// else, keeping manifest order inside each stage: a pod naming a missing
// PriorityClass is rejected at admission, which is why priorityclasses.yaml
// also leads deploy/k8s/kustomization.yaml.
func staged(objs ports.Objects) ports.Objects {
	out := make(ports.Objects, 0, len(objs))
	var namespaced ports.Objects
	for _, o := range objs {
		if allowed[groupKind(o)] == scopeCluster {
			out = append(out, o)
			continue
		}
		namespaced = append(namespaced, o)
	}
	return append(out, namespaced...)
}
