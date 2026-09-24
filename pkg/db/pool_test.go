// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJitteredConnMaxLifetimeStaysInRange(t *testing.T) {
	upper := connMaxLifetime + connMaxLifetimeJitter

	for i := 0; i < 100; i++ {
		got := jitteredConnMaxLifetime()
		require.GreaterOrEqual(t, got, connMaxLifetime)
		require.Less(t, got, upper)
	}
}

func TestJitteredConnMaxLifetimeVaries(t *testing.T) {
	seen := make(map[int64]struct{})
	for i := 0; i < 100; i++ {
		seen[int64(jitteredConnMaxLifetime())] = struct{}{}
	}

	require.Greater(t, len(seen), 1)
}
