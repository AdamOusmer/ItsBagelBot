// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// --- Loyalty ----------------------------------------------------------------
// The loyalty economy: viewers earn points from subs, resubs, gift subs,
// cheers and watch time (a 5-minute tick over everyone in chat while live).
// Rates live in the "loyalty" module blob; standings and counters live in the
// loyalty service (bagel.rpc.loyalty.*).

// LoyaltyConfig mirrors sesame's LoyaltyModuleConfig blob: 0 means "use the
// default", a negative value switches that source off.
export interface LoyaltyConfig {
  pointsName: string;
  subPoints: number;
  resubPoints: number;
  giftSubPoints: number;
  cheerPointsPer100: number;
  watchPointsPerTick: number;
}

// LOYALTY_DEFAULTS are the effective rates behind a zero value, mirrored from
// sesame so the form can show what "default" means.
export const LOYALTY_DEFAULTS: LoyaltyConfig = {
  pointsName: 'points',
  subPoints: 500,
  resubPoints: 500,
  giftSubPoints: 100,
  cheerPointsPer100: 50,
  watchPointsPerTick: 10
};

export function blankLoyaltyConfig(): LoyaltyConfig {
  return { pointsName: '', subPoints: 0, resubPoints: 0, giftSubPoints: 0, cheerPointsPer100: 0, watchPointsPerTick: 0 };
}

// The broadcaster-facing counter scopes. 'command' pools every viewer into
// one total per command/reward; 'viewer_command' keeps one value per viewer
// per command. (A fifth, admin-only 'bot' scope exists service-side and never
// surfaces in the dashboard.)
export type CounterScope = 'channel' | 'viewer' | 'command' | 'viewer_command';
export const COUNTER_SCOPES: readonly CounterScope[] = ['channel', 'viewer', 'command', 'viewer_command'];

// CounterDef is one counter definition as counter.list returns it; value is
// the channel-scope tally (entry scopes keep per-viewer values instead).
export interface CounterDef {
  name: string;
  scope: CounterScope;
  value: number;
}

// CounterEntryView is one stored bucket of an entry-scoped counter (the
// per-counter leaderboard row).
export interface CounterEntryView {
  viewerId: string;
  viewerLogin: string;
  viewerName: string;
  command: string;
  value: number;
}

// LoyaltyStanding is one viewer's points + watch time (the channel top list).
export interface LoyaltyStanding {
  viewerId: string;
  viewerLogin: string;
  viewerName: string;
  points: number;
  watchSeconds: number;
}
