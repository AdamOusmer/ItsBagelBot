// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import "testing"

func TestLevelOf(t *testing.T) {
	cases := []struct {
		xp   int64
		want int
	}{
		{-1, 0},
		{0, 0},
		{1, 0},
		{99, 0},
		// Exact level boundaries: level N begins at 100*N^2.
		{100, 1},
		{399, 1},
		{400, 2},
		{899, 2},
		{900, 3},
		{10000, 10},
		{1000000, 100},
	}
	for _, tc := range cases {
		if got := LevelOf(tc.xp); got != tc.want {
			t.Fatalf("LevelOf(%d) = %d, want %d", tc.xp, got, tc.want)
		}
	}
}

// TestLevelOfIsMonotonic guards the property the level-up edge depends on: XP
// only ever moves the level up, never down.
func TestLevelOfIsMonotonic(t *testing.T) {
	previous := 0
	for xp := int64(0); xp <= 5000; xp++ {
		level := LevelOf(xp)
		if level < previous {
			t.Fatalf("LevelOf(%d) = %d went below the previous level %d", xp, level, previous)
		}
		previous = level
	}
}
