// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package apply

import (
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"ItsBagelBot/app/deployer/internal/ports"
)

// A service is one rollout unit: a Deployment or DaemonSet and the objects
// that travel with it. An object belongs to service S when it sits in S's
// namespace and is named S or S-<suffix> (the longest matching S wins), which
// is the naming every deploy/k8s file already follows: commands-env,
// twitch-ingress-headless, notifications-cleanup, console-dashboard-transport,
// the ScaledObjects named after their Deployment. Grouping by source file was
// rejected: discord.yaml holds three Deployments that roll at different
// points of the train, and kustomize output carries no file origin unless the
// kustomization opts into buildMetadata.
var serviceKinds = map[schema.GroupKind]bool{
	{Group: "apps", Kind: "Deployment"}: true,
	{Group: "apps", Kind: "DaemonSet"}:  true,
}

type services map[string]ports.Namespace

func servicesOf(objs ports.Objects) services {
	out := services{}
	for _, o := range objs {
		if serviceKinds[groupKind(o)] {
			out[o.GetName()] = ports.Namespace(o.GetNamespace())
		}
	}
	return out
}

// owner is the service o belongs to, "" for a shared object.
func (s services) owner(o *unstructured.Unstructured) string {
	ns := ports.Namespace(o.GetNamespace())
	cand := o.GetName()
	for {
		if home, ok := s[cand]; ok && home == ns {
			return cand
		}
		i := strings.LastIndexByte(cand, '-')
		if i < 0 {
			return ""
		}
		cand = cand[:i]
	}
}

// Services lists the services in objs: those named in order first, in that
// order, then any order does not name, in manifest order, so a newly added
// Deployment still rolls (last) instead of being skipped.
func Services(objs ports.Objects, order []string) []string {
	present := servicesOf(objs)
	out := slices.DeleteFunc(slices.Clone(order), func(name string) bool {
		_, ok := present[name]
		return !ok
	})
	for _, name := range manifestOrder(objs) {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// manifestOrder lists the service workloads in objs by name, in manifest order.
func manifestOrder(objs ports.Objects) []string {
	var out []string
	for _, o := range objs {
		if serviceKinds[groupKind(o)] {
			out = append(out, o.GetName())
		}
	}
	return out
}

// ForService returns the objects of one service, in manifest order.
func ForService(objs ports.Objects, name string) ports.Objects {
	return owned(objs, func(owner string) bool { return owner == name })
}

// Shared returns the objects no service owns (PriorityClasses, the
// namespace-wide NetworkPolicies, shared Middlewares and IngressRoutes, backup
// CronJobs), in manifest order.
func Shared(objs ports.Objects) ports.Objects {
	return owned(objs, func(owner string) bool { return owner == "" })
}

func owned(objs ports.Objects, keep func(owner string) bool) ports.Objects {
	s := servicesOf(objs)
	var out ports.Objects
	for _, o := range objs {
		if keep(s.owner(o)) {
			out = append(out, o)
		}
	}
	return out
}
