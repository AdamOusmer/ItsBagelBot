// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"maps"
	"testing"
	"time"
)

// TestHandlerBudgets pins the three budgets the notifications verbs run on.
// They were positional arguments to QueueSubscribeJSON before the shared
// wiring landed, which is exactly the kind of value a refactor flattens onto
// one default without anyone noticing until the janitor starts timing out.
func TestHandlerBudgets(t *testing.T) {
	got := map[string]time.Duration{
		"read":    readBudget,
		"send":    sendBudget,
		"cleanup": cleanupBudget,
	}
	want := map[string]time.Duration{
		"read":    3 * time.Second,
		"send":    5 * time.Second,
		"cleanup": 30 * time.Second,
	}
	if !maps.Equal(got, want) {
		t.Fatalf("handler budgets = %v, want %v", got, want)
	}
}
