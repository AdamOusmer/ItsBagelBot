// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLevelOf(t *testing.T) {
	cases := map[int64]int{
		-1: 0, 0: 0, 1: 0, 99: 0,
		100: 1, 399: 1,
		400: 2, 899: 2,
		900: 3, 10000: 10, 1000000: 100,
	}
	for xp, want := range cases {
		assert.Equal(t, want, LevelOf(xp), "LevelOf(%d)", xp)
	}

	previous := 0
	for xp := int64(0); xp <= 5000; xp++ {
		level := LevelOf(xp)
		if level < previous {
			t.Fatalf("LevelOf(%d) = %d went below the previous level %d", xp, level, previous)
		}
		previous = level
	}
}
