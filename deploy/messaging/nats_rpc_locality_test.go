// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"slices"
	"strings"
	"testing"

	"ItsBagelBot/internal/natsacl"
)

func TestExactRPCGrantsIncludeNodeLocalVariant(t *testing.T) {
	counts := serviceGrantCounts(committedACL(t))
	exact := exactServiceGrants(counts)

	for subject, count := range exact {
		local := subject + ".node.*"
		if counts[local] < count {
			t.Errorf("exact service grant %q occurs %d times; local grant %q occurs %d times",
				subject, count, local, counts[local])
		}
	}
	if len(exact) < 10 {
		t.Fatalf("checked only %d exact RPC grants; accounts.yaml likely lost its service grants", len(exact))
	}
}

func serviceGrantCounts(acl *natsacl.ACL) map[string]int {
	counts := make(map[string]int)
	for _, spec := range acl.Accounts {
		for _, subject := range serviceSubjects(spec) {
			counts[subject]++
		}
	}
	return counts
}

func serviceSubjects(spec natsacl.AccountSpec) []string {
	var subjects []string
	for _, exp := range spec.Exports {
		subjects = append(subjects, exp.Service)
	}
	for _, imp := range spec.Imports {
		subjects = append(subjects, imp.Service)
	}
	return slices.DeleteFunc(subjects, func(s string) bool { return s == "" })
}

func exactServiceGrants(counts map[string]int) map[string]int {
	exact := make(map[string]int)
	for subject, count := range counts {
		if !strings.HasSuffix(subject, ".>") && !strings.HasSuffix(subject, ".node.*") {
			exact[subject] = count
		}
	}
	return exact
}
