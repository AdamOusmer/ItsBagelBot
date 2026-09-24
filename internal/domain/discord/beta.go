// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discord

const BetaPremiumOnly = true

func PremiumGateOpen(status string, known bool) bool {
	if !BetaPremiumOnly {
		return true
	}
	return known && IsPremium(status)
}
