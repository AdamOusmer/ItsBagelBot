// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

import "math"

// xpPerLevelSquared is the divisor of the level curve: level = floor(sqrt(xp/100)),
// i.e. level N starts at 100*N^2 XP. It moved here from
// internal/discordstore's unexported levelOf when the XP rows moved to MySQL
// (app/db/discord): the discord-data repository computes the stored level
// column on every upsert, and the engine renders the same number from its
// Valkey fast path, so the two had to stop owning separate copies of the
// curve. A drift there is invisible -- both sides look right in isolation and
// disagree only on the card a member sees after a level-up announcement.
//
// The shape is deliberately quadratic, not linear: at 15 XP per message with a
// 60s cooldown, a linear curve makes every level cost the same handful of
// messages and the number stops meaning anything past a week of chat.
const xpPerLevelSquared = 100

// LevelOf maps lifetime XP to a level. Non-positive XP is level 0, so a
// missing or reset row and a brand-new member read the same. Truncating rather
// than rounding is load-bearing: level N must begin exactly at 100*N^2, or the
// "leveled up" edge in AddXP fires half a level early.
func LevelOf(xp int64) int {
	if xp <= 0 {
		return 0
	}
	return int(math.Sqrt(float64(xp) / xpPerLevelSquared))
}
