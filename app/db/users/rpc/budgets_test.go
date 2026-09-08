// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"maps"
	"testing"
	"time"
)

// TestHandlerBudgets pins the five budgets the users verbs run on, the way
// notifications pins its three. Before the shared wiring they were positional
// arguments spread over six files; a flattening onto one default is silent
// until production starts timing out.
func TestHandlerBudgets(t *testing.T) {
	got := map[string]time.Duration{
		"tokens":  tokensBudget,
		"email":   emailBudget,
		"counts":  countsBudget,
		"admin":   adminBudget,
		"billing": billingBudget,
	}
	want := map[string]time.Duration{
		"tokens":  2 * time.Second,
		"email":   3 * time.Second,
		"counts":  3 * time.Second,
		"admin":   3 * time.Second,
		"billing": 5 * time.Second,
	}
	if !maps.Equal(got, want) {
		t.Fatalf("handler budgets = %v, want %v", got, want)
	}
}
