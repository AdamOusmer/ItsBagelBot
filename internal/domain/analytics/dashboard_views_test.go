// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package analytics_test

import (
	"testing"

	catalog "ItsBagelBot/internal/domain/analytics"
	"github.com/stretchr/testify/require"
)

func TestDashboardVoiceViewRoles(t *testing.T) {
	for _, tc := range []struct {
		role  string
		valid bool
	}{
		{"owner", true},
		{"delegate", true},
		{"123456789", false},
	} {
		t.Run(tc.role, func(t *testing.T) {
			require.Equal(t, tc.valid, catalog.Valid(catalog.DashboardViews, catalog.Counter, []string{"voice", tc.role}, nil))
		})
	}
}
