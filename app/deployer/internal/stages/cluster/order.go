// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package cluster

import (
	"maps"
	"slices"

	"ItsBagelBot/app/deployer/internal/ports"
)

// self is the deployer's own unit.
const self = "deployer"

// slot is one known rollout unit.
type slot struct {
	name string
	ns   ports.Namespace
}

// order is the fixed rollout order, one service at a time. The tiers follow
// who calls whom: db services own the RPC verbs the backends call, the
// backends consume what the ingresses publish, and the consoles call all of
// them. A caller rolled before its callee sends requests the old callee
// answers with an unknown-verb refusal for the whole gap. Namespaces are
// contiguous here, which sequence relies on to append unknown services after
// the last known one of their namespace.
var order = []slot{
	{"commands", nsDB}, {"loyalty", nsDB}, {"modules", nsDB}, {"notifications", nsDB},
	{"discord-data", nsDB}, {"projector", nsDB}, {"transactions", nsDB}, {"users", nsDB},

	{"discord-engine", nsApp}, {"discord-outgress", nsApp}, {"gossip", nsApp}, {"outgress", nsApp}, {"sesame", nsApp},
	{"discord-ingress", nsApp}, {"twitch-ingress", nsApp},
	{"console-dashboard", nsApp}, {"console-admin", nsApp},
}

// Order returns the rollout units in rollout order: db namespace services,
// app backends, ingresses, consoles, deployer last. Rolling the deployer
// replaces the pod running the train, so nothing may come after it but the
// status routes a resumed run applies.
func Order() []string {
	names := make([]string, 0, len(order)+1)
	for _, s := range order {
		names = append(names, s.name)
	}
	return append(names, self)
}

// sequence puts the units found in a build in rollout order. A service the
// table does not know yet goes after the last known one of its namespace,
// by name; one in a namespace the table does not know goes before the
// deployer.
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
