// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/gossip/internal/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchBudgetLeavesTimeForTheReply(t *testing.T) {
	for _, budget := range []time.Duration{100 * time.Millisecond, 10 * time.Second} {
		t.Run(budget.String(), func(t *testing.T) {
			parent, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			fetch, finish := core.FetchContext(parent)
			defer finish()
			parentDeadline, _ := parent.Deadline()
			fetchDeadline, ok := fetch.Deadline()
			require.True(t, ok)
			margin := parentDeadline.Sub(fetchDeadline)
			assert.Positive(t, margin)
			assert.LessOrEqual(t, margin, 100*time.Millisecond)
			assert.Less(t, margin, budget)
		})
	}
}

func TestFetchBudgetPreservesParentCancellation(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	fetch, finish := core.FetchContext(parent)
	defer finish()
	cancel()
	assert.ErrorIs(t, fetch.Err(), context.Canceled)
}
