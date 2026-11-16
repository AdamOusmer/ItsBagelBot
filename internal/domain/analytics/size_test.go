// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"testing"

	catalog "ItsBagelBot/internal/domain/analytics"
	"github.com/stretchr/testify/require"
)

func TestChannelsSizeCatalog(t *testing.T) {
	spec, ok := catalog.Lookup(catalog.ChannelsSize)
	require.True(t, ok)
	require.Equal(t, catalog.Spec{Type: catalog.Distinct, Dims: []catalog.Kind{catalog.KindSize}, Windows: catalog.Week | catalog.Month}, spec)
	for _, tc := range []struct {
		dim   string
		valid bool
	}{
		{"v0_9", true}, {"v10_49", true}, {"v50_199", true}, {"v200_999", true}, {"v1kp", true},
		{"123456789", false}, {"streamer-login", false}, {"private|text", false}, {"", false},
	} {
		t.Run(tc.dim, func(t *testing.T) {
			require.Equal(t, tc.valid, catalog.Valid(catalog.ChannelsSize, catalog.Distinct, []string{tc.dim}, nil))
		})
	}
}
