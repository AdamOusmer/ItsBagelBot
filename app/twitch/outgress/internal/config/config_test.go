// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadRateRegion(t *testing.T) {
	tests := []struct{ name, region, want string }{
		{"falls back to local", "", "local"},
		{"keeps an explicit locality", "node2", "node2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TWITCH_CLIENT_ID", "test-client")
			t.Setenv("TWITCH_CLIENT_SECRET", "test-secret")
			t.Setenv("OUTGRESS_REGION", tt.region)

			assert.Equal(t, tt.want, Load().RateRegion)
		})
	}
}
