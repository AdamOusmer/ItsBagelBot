// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package db

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestJitteredConnMaxLifetimeStaysInRange asserts the half-open window the
// decision record on connMaxLifetimeJitter promises, 30 to 40 minutes. A draw
// below connMaxLifetime would recycle connections earlier than intended, and a
// draw at or above the upper bound would widen the window the wait_timeout
// assumption in provider.go depends on.
func TestJitteredConnMaxLifetimeStaysInRange(t *testing.T) {
	upper := connMaxLifetime + connMaxLifetimeJitter

	for i := 0; i < 100; i++ {
		got := jitteredConnMaxLifetime()
		require.GreaterOrEqual(t, got, connMaxLifetime)
		require.Less(t, got, upper)
	}
}

// TestJitteredConnMaxLifetimeVaries guards against the whole point of the
// change being lost to a constant offset: if every pod drew the same value the
// recycle clocks would stay phase-aligned exactly as they were with a fixed
// lifetime. 100 draws over a 10 minute range collide on every value only if
// the source is not random at all.
func TestJitteredConnMaxLifetimeVaries(t *testing.T) {
	seen := make(map[int64]struct{})
	for i := 0; i < 100; i++ {
		seen[int64(jitteredConnMaxLifetime())] = struct{}{}
	}

	require.Greater(t, len(seen), 1)
}
