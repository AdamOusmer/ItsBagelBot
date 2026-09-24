// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import "math"

const xpPerLevelSquared = 100

func LevelOf(xp int64) int {
	if xp <= 0 {
		return 0
	}
	return int(math.Sqrt(float64(xp) / xpPerLevelSquared))
}
