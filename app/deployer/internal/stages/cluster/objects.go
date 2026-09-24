// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cluster

import (
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"ItsBagelBot/app/deployer/internal/ports"
	"ItsBagelBot/internal/domain/rpc/deploy"
)

const (
	nsApp       ports.Namespace = "app"
	nsDB        ports.Namespace = "db"
	nsMessaging ports.Namespace = "messaging"
	nsOps       ports.Namespace = "ops"

	kindDeployment    = "Deployment"
	kindDaemonSet     = "DaemonSet"
	kindCronJob       = "CronJob"
	kindConfigMap     = "ConfigMap"
	kindPriorityClass = "PriorityClass"
	kindIngressRoute  = "IngressRoute"
)

var (
	managedKinds = map[string]bool{
		kindDeployment: true, kindDaemonSet: true, kindCronJob: true, "Service": true,
		kindConfigMap: true, "PodDisruptionBudget": true, "NetworkPolicy": true,
		kindPriorityClass: true, kindIngressRoute: true, "Middleware": true, "ScaledObject": true,
	}
	managedNamespaces = map[ports.Namespace]bool{nsApp: true, nsDB: true, nsMessaging: true}
)

func managed(o *unstructured.Unstructured) bool {
	if o.GetKind() == kindPriorityClass {
		return true
	}
	return managedKinds[o.GetKind()] && managedNamespaces[namespaceOf(o)]
}

func managedOnly(objs ports.Objects) ports.Objects {
	return slices.DeleteFunc(slices.Clone(objs), func(o *unstructured.Unstructured) bool { return !managed(o) })
}

func namespaceOf(o *unstructured.Unstructured) ports.Namespace {
	return ports.Namespace(o.GetNamespace())
}

func refOf(o *unstructured.Unstructured) ports.ObjectRef {
	return ports.ObjectRef{Kind: o.GetKind(), Namespace: namespaceOf(o), Name: o.GetName()}
}

func workloadRef(o *unstructured.Unstructured) ports.WorkloadRef {
	return ports.WorkloadRef{Kind: o.GetKind(), Namespace: namespaceOf(o), Name: o.GetName()}
}

func rolls(o *unstructured.Unstructured) bool {
	return o.GetKind() == kindDeployment || o.GetKind() == kindDaemonSet
}

var podSpecPath = map[string][]string{
	kindDeployment: {"spec", "template", "spec"},
	kindDaemonSet:  {"spec", "template", "spec"},
	kindCronJob:    {"spec", "jobTemplate", "spec", "template", "spec"},
}

type container struct {
	name  string
	image string
}

func (c container) pin(repo string) (deploy.ImageName, deploy.Digest, bool) {
	rest, ok := strings.CutPrefix(c.image, repo+"/")
	if !ok {
		return "", "", false
	}
	ref, digest, ok := strings.Cut(rest, "@")
	name, _, _ := strings.Cut(ref, ":")
	return deploy.ImageName(name), deploy.Digest(digest), ok
}

func containersOf(o *unstructured.Unstructured) []container {
	spec, ok := podSpecPath[o.GetKind()]
	if !ok {
		return nil
	}
	init := containersAt(o, append(slices.Clip(spec), "initContainers"))
	return append(init, containersAt(o, append(slices.Clip(spec), "containers"))...)
}

func containersAt(o *unstructured.Unstructured, fields []string) []container {
	list, _, _ := unstructured.NestedSlice(o.Object, fields...)
	out := make([]container, 0, len(list))
	for _, raw := range list {
		m, _ := raw.(map[string]any)
		name, _, _ := unstructured.NestedString(m, "name")
		image, _, _ := unstructured.NestedString(m, "image")
		out = append(out, container{name: name, image: image})
	}
	return out
}

type unit struct {
	name      string
	ns        ports.Namespace
	managed   bool
	workloads []*unstructured.Unstructured
	objs      ports.Objects
}

func (u *unit) refs() []ports.WorkloadRef {
	refs := make([]ports.WorkloadRef, len(u.workloads))
	for i, w := range u.workloads {
		refs[i] = workloadRef(w)
	}
	return refs
}

func (u *unit) pins(repo string) ports.Pins {
	pins := ports.Pins{}
	for _, c := range containersIn(u.workloads) {
		if name, digest, ok := c.pin(repo); ok {
			pins[name] = digest
		}
	}
	return pins
}

func (u *unit) claims(o *unstructured.Unstructured) bool {
	if u.ns != namespaceOf(o) {
		return false
	}
	name := o.GetName()
	return name == u.name || strings.HasPrefix(name, u.name+"-")
}

func (u *unit) longer(than *unit) bool { return than == nil || len(u.name) > len(than.name) }

func containersIn(workloads []*unstructured.Unstructured) []container {
	var out []container
	for _, w := range workloads {
		out = append(out, containersOf(w)...)
	}
	return out
}

func workloadsOf(units []*unit) []*unstructured.Unstructured {
	var out []*unstructured.Unstructured
	for _, u := range units {
		out = append(out, u.workloads...)
	}
	return out
}

func refsOf(units []*unit) []ports.WorkloadRef {
	var refs []ports.WorkloadRef
	for _, u := range units {
		refs = append(refs, u.refs()...)
	}
	return refs
}

func managedUnits(units []*unit) []*unit {
	return slices.DeleteFunc(slices.Clone(units), func(u *unit) bool { return !u.managed })
}

type layout struct {
	priority  ports.Objects
	shared    ports.Objects
	units     []*unit
	unmanaged []ports.ObjectRef
}

func arrange(objs ports.Objects) layout {
	units := unitsOf(objs)
	var l layout
	for _, o := range objs {
		l.place(o, units)
	}
	l.units = sequence(units)
	return l
}

func unitsOf(objs ports.Objects) map[string]*unit {
	units := map[string]*unit{}
	for _, o := range objs {
		if rolls(o) {
			u := unitFor(units, o)
			u.workloads = append(u.workloads, o)
		}
	}
	return units
}

func unitFor(units map[string]*unit, o *unstructured.Unstructured) *unit {
	if u, ok := units[o.GetName()]; ok {
		return u
	}
	u := &unit{name: o.GetName(), ns: namespaceOf(o), managed: managed(o)}
	units[u.name] = u
	return u
}

func (l *layout) place(o *unstructured.Unstructured, units map[string]*unit) {
	switch {
	case o.GetKind() == kindPriorityClass:
		l.priority = append(l.priority, o)
	case !managed(o):
		l.unmanaged = append(l.unmanaged, refOf(o))
	default:
		l.attach(o, units)
	}
}

func (l *layout) attach(o *unstructured.Unstructured, units map[string]*unit) {
	var best *unit
	for _, u := range units {
		if u.claims(o) && u.longer(best) {
			best = u
		}
	}
	if best == nil {
		l.shared = append(l.shared, o)
		return
	}
	best.objs = append(best.objs, o)
}
