// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"ItsBagelBot/internal/analytics"
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestPagesWalksEveryRowInBoundedPages(t *testing.T) {
	rows := make([]int, 2500)
	for i := range rows {
		rows[i] = i + 1
	}
	var seen, calls int
	fetch := func(_ context.Context, after *int, limit int) ([]int, error) {
		calls++
		start := 0
		if after != nil {
			start = *after
		}
		return rows[start:min(start+limit, len(rows))], nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	require.NoError(t, analytics.Pages(ctx, fetch, func(n int) int { return n }, func(int) { seen++ }))
	require.Equal(t, 2500, seen)
	require.Equal(t, 3, calls)
}
