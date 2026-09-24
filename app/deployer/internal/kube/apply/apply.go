// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package apply

import (
	"context"
	"fmt"
	"maps"
	"reflect"

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

const FieldManager = "bagel-deployer"

type Applier struct {
	dyn    dynamic.Interface
	mapper meta.RESTMapper
}

var _ ports.Applier = (*Applier)(nil)

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

func (a *Applier) Build(_ context.Context, spec ports.BuildSpec) (ports.Objects, error) {
	return build(spec)
}

func (a *Applier) Lint(objs ports.Objects) []ports.LintFinding { return lint(objs) }

func (a *Applier) Apply(ctx context.Context, objs ports.Objects) (ports.ApplyResult, error) {
	if err := refuse(objs); err != nil {
		return ports.ApplyResult{}, err
	}
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

func (a *Applier) applyOne(ctx context.Context, o *unstructured.Unstructured) (bool, error) {
	ri, err := a.resource(o)
	if err != nil {
		return false, err
	}
	before, err := current(ctx, ri, o)
	if err != nil {
		return false, err
	}
	after, err := ri.Apply(ctx, o.GetName(), o, metav1.ApplyOptions{FieldManager: FieldManager, Force: true})
	if err != nil {
		return false, err
	}
	return before == nil || !reflect.DeepEqual(content(before), content(after)), nil
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

func current(ctx context.Context, ri dynamic.ResourceInterface, o *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	live, err := ri.Get(ctx, o.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	return live, err
}

// A forced apply that only takes field ownership moves resourceVersion and managedFields; that is not a change.
func content(o *unstructured.Unstructured) map[string]any {
	c := maps.Clone(o.Object)
	delete(c, "metadata")
	delete(c, "status")
	c["labels"], c["annotations"] = o.GetLabels(), o.GetAnnotations()
	return c
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
