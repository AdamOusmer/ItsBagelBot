// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package eligibility

import "time"

type RuleLimits struct {
	MaxPoints      int64
	MaxWatchHours  uint64
	MaxFollowDays  int
	MaxAccountDays int
	MaxSkipDays    int
	LuckMin        int
	LuckMax        int
	CheckCooldown  time.Duration
	RefusalEnabled bool
}

var Limits = RuleLimits{
	MaxPoints: 1_000_000_000, MaxWatchHours: 10_000, MaxFollowDays: 3650,
	MaxAccountDays: 3650, MaxSkipDays: 90, LuckMin: 100, LuckMax: 300,
	CheckCooldown: 10 * time.Second, RefusalEnabled: true,
}
