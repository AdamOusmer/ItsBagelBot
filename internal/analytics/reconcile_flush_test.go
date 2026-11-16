// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"context"
	"testing"

	"ItsBagelBot/internal/analytics"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/valkeytest"

	"github.com/stretchr/testify/require"
)

func TestReconcileFlushesPendingDeltasBeforeSeed(t *testing.T) {
	for _, delta := range []int64{1, -1} {
		t.Run(map[int64]string{1: "increase", -1: "decrease"}[delta], func(t *testing.T) {
			ctx := context.Background()
			client := valkeytest.Spawn(t)
			at := day
			r := newRecorder(t, client, &at)
			field := catalog.Field(catalog.AccountsTotal, "free")
			r.Shift(nil, analytics.Tally{field: 100})
			r.Flush(ctx)
			r.Shift(analytics.Tally{field: 100}, analytics.Tally{field: 100 + delta})

			var seen analytics.Tally
			r.Reconcile(ctx, analytics.Owner{
				Service: "users",
				Owned:   analytics.Owned{string(catalog.AccountsTotal)},
				Seed: func(context.Context) (analytics.Tally, error) {
					seen = levels(t, client)
					return analytics.Tally{field: 100 + delta}, nil
				},
			})
			r.Flush(ctx)

			want := analytics.Tally{field: 100 + delta}
			require.Equal(t, want, seen)
			require.Equal(t, map[string]int64(want), levels(t, client))
		})
	}
}
