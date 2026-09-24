// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cluster

import (
	"maps"
	"slices"

	"ItsBagelBot/app/deployer/internal/ports"
)

const self = "deployer"

type slot struct {
	name string
	ns   ports.Namespace
}

// Callees roll before callers; namespaces must stay contiguous for sequence.
var order = []slot{
	{"commands", nsDB}, {"loyalty", nsDB}, {"modules", nsDB}, {"notifications", nsDB},
	{"discord-data", nsDB}, {"projector", nsDB}, {"transactions", nsDB}, {"users", nsDB},

	{"discord-engine", nsApp}, {"discord-outgress", nsApp}, {"gossip", nsApp}, {"outgress", nsApp}, {"sesame", nsApp},
	{"discord-ingress", nsApp}, {"twitch-ingress", nsApp},
	{"console-dashboard", nsApp}, {"console-admin", nsApp},
}

// The deployer must stay last: rolling it replaces the pod running the train.
func Order() []string {
	names := make([]string, 0, len(order)+1)
	for _, s := range order {
		names = append(names, s.name)
	}
	return append(names, self)
}

func sequence(found map[string]*unit) []*unit {
	unknown := unknownByNamespace(found)
	out := make([]*unit, 0, len(found))
	for i, s := range order {
		if u, ok := found[s.name]; ok {
			out = append(out, u)
		}
		if lastOfNamespace(i) {
			out = append(out, unknown[s.ns]...)
			delete(unknown, s.ns)
		}
	}
	for _, ns := range slices.Sorted(maps.Keys(unknown)) {
		out = append(out, unknown[ns]...)
	}
	if u, ok := found[self]; ok {
		out = append(out, u)
	}
	return out
}

func lastOfNamespace(i int) bool {
	return i == len(order)-1 || order[i+1].ns != order[i].ns
}

func unknownByNamespace(found map[string]*unit) map[ports.Namespace][]*unit {
	known := map[string]bool{self: true}
	for _, s := range order {
		known[s.name] = true
	}
	unknown := map[ports.Namespace][]*unit{}
	for _, name := range slices.Sorted(maps.Keys(found)) {
		if !known[name] {
			u := found[name]
			unknown[u.ns] = append(unknown[u.ns], u)
		}
	}
	return unknown
}
