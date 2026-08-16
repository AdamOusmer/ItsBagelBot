// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"
	"time"
)

// contractClause is one clause of a shipped-config contract: the condition the
// config has to satisfy and the failure that states what the broker does when it
// does not. Stating the clauses as data keeps a contract with many terms one
// assertion instead of one branch per term.
type contractClause struct {
	satisfied bool
	failure   string
}

// requireContract fails on the first unsatisfied clause, in the order the
// clauses are stated.
func requireContract(t *testing.T, clauses ...contractClause) {
	t.Helper()
	for _, clause := range clauses {
		if !clause.satisfied {
			t.Fatal(clause.failure)
		}
	}
}

// awaitSignal waits for a signal the test expects to arrive, and names what its
// absence means.
func awaitSignal[T any](t *testing.T, signal <-chan T, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}
