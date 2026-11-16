// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"testing"

	catalog "ItsBagelBot/internal/domain/analytics"
	"github.com/stretchr/testify/require"
)

func TestBeaconCatalog(t *testing.T) {
	for _, tc := range []struct {
		metric catalog.Metric
		kind   catalog.Kind
		values []string
	}{
		{catalog.PublicViews, catalog.KindPage, []string{"commands", "leaderboard", "songqueue", "queue", "stats", "login"}},
		{catalog.MarketingCTA, catalog.KindCTA, []string{"nav", "hero", "playground", "finale", "pricing", "footer", "landing", "compare", "guide", "import", "builder", "app"}},
	} {
		t.Run(string(tc.metric), func(t *testing.T) {
			spec, ok := catalog.Lookup(tc.metric)
			require.True(t, ok)
			require.Equal(t, catalog.Spec{Type: catalog.Counter, Dims: []catalog.Kind{tc.kind}}, spec)
			require.Equal(t, tc.values, catalog.Enum(tc.kind))
			for _, value := range tc.values {
				require.True(t, catalog.Valid(tc.metric, catalog.Counter, []string{value}, nil))
			}
			for _, dims := range [][]string{{"123456789"}, {"streamer-login"}, {"private|text"}, {""}, {}, {tc.values[0], "extra"}} {
				require.False(t, catalog.Valid(tc.metric, catalog.Counter, dims, nil))
			}
		})
	}
}
