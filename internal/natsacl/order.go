// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsacl

import "sort"

// orderAccounts returns account names exporters-first. Ties and cycles break
// on lexicographic name order so the result is deterministic.
func orderAccounts(acl *ACL) []string {
	names := sortedAccountNames(acl)
	edges, inDegree := importEdges(acl, names)
	remaining := make(map[string]bool, len(names))
	for _, name := range names {
		remaining[name] = true
	}

	order := make([]string, 0, len(names))
	for len(remaining) > 0 {
		next := pickNext(names, remaining, inDegree)
		order = append(order, next)
		delete(remaining, next)
		for _, target := range edges[next] {
			if remaining[target] {
				inDegree[target]--
			}
		}
	}
	return order
}

func sortedAccountNames(acl *ACL) []string {
	names := make([]string, 0, len(acl.Accounts))
	for name := range acl.Accounts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// importEdges maps exporter -> importers it must precede, deduplicated per pair.
func importEdges(acl *ACL, names []string) (map[string][]string, map[string]int) {
	edges := make(map[string][]string, len(names))
	inDegree := make(map[string]int, len(names))
	seenPair := make(map[[2]string]bool)
	for _, name := range names {
		for _, imp := range acl.Accounts[name].Imports {
			from := imp.From
			pair := [2]string{from, name}
			if from == name || seenPair[pair] {
				continue
			}
			seenPair[pair] = true
			edges[from] = append(edges[from], name)
			inDegree[name]++
		}
	}
	return edges, inDegree
}

// pickNext returns the lexicographically smallest ready account, or the
// lexicographically smallest remaining one to break a cycle.
func pickNext(names []string, remaining map[string]bool, inDegree map[string]int) string {
	fallback := ""
	for _, name := range names {
		if !remaining[name] {
			continue
		}
		if fallback == "" {
			fallback = name
		}
		if inDegree[name] <= 0 {
			return name
		}
	}
	return fallback
}
