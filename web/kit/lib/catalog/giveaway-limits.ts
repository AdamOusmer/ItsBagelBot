// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const GIVEAWAY_LIMITS = {
  maxPoints: 1_000_000_000,
  maxWatchHours: 10_000,
  maxFollowDays: 3650,
  maxAccountDays: 3650,
  maxSkipDays: 90,
  luckMin: 100,
  luckMax: 300,
  refusalEnabled: true,
  maxWinners: 100,
  giveawayDefaultWinners: 1,
  giveawayEntryWindow: 180,
  giveawayMinEntryWindow: 1,
  giveawayMaxPresets: 10,
  giveawayPresetNameMax: 24,
  giveawayPrizeMaxLen: 100,
  claimWindowMin: 30,
  claimWindowMax: 900,
  claimWindowDefault: 120,
  maxOpen: 7200,
  giveawayRoundIDMaxLen: 36,
  giveawayMaxRecentPage: 50
} as const;
