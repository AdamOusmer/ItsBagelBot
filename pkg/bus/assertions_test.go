// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"
	"time"
)

type contractClause struct {
	satisfied bool
	failure   string
}

func requireContract(t *testing.T, clauses ...contractClause) {
	t.Helper()
	for _, clause := range clauses {
		if !clause.satisfied {
			t.Fatal(clause.failure)
		}
	}
}

func awaitSignal[T any](t *testing.T, signal <-chan T, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal(message)
	}
}

func waitFor(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(message)
}
