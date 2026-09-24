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

func manifestOrder(objs ports.Objects) []string {
	var out []string
	for _, o := range objs {
		if serviceKinds[groupKind(o)] {
			out = append(out, o.GetName())
		}
	}
	return out
}

func ForService(objs ports.Objects, name string) ports.Objects {
	return owned(objs, func(owner string) bool { return owner == name })
}

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
