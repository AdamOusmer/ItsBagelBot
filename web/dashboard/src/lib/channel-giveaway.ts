// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { GIVEAWAY_LIMITS as pins } from '@bagel/kit';

export interface GiveawayRules {
  role?: 'everyone' | 'sub' | 'vip';
  exclude_mods?: boolean;
  min_points?: number;
  min_watch_seconds?: number;
  followers_only?: boolean;
  min_follow_days?: number;
  min_account_days?: number;
  skip_winners_days?: number;
  sub_luck_percent?: number;
}

export interface GiveawayPreset {
  name: string;
  rules: GiveawayRules;
  freezeWhileOpen?: boolean;
}

export interface GiveawayConfig {
  defaultPreset: string;
  presets: GiveawayPreset[];
  fields: Record<string, string>;
}

export interface GiveawayCandidate {
  viewer_id: string;
  login?: string;
  generation: number;
  reason: string;
  detail?: Record<string, string>;
}

export type GiveawaySlotState = 'pending' | 'verifying' | 'won' | 'claimed' | 'no_show' | 'unfilled' | 'ended';

export interface GiveawaySlot {
  n: number;
  generation: number;
  viewer_id?: string;
  login?: string;
  state: GiveawaySlotState;
  deadline_ms?: number;
  can_skip_rule?: boolean;
  rejected?: GiveawayCandidate[];
}

export interface GiveawayRound {
  id: string;
  kind: 'giveaway';
  phase: 'open' | 'closing' | 'settling';
  prize: string;
  winners: number;
  opened_ms: number;
  closes_ms: number;
  freeze_ends_ms?: number;
  entries: number;
  refused: number;
  slots: GiveawaySlot[];
  requirements: string[];
}

export interface GiveawayWinner {
  round_id: string;
  slot: number;
  viewer_id: string;
  login: string;
  prize: string;
  won_at: number;
  claimed: boolean;
  claimed_at?: number;
  delivered_at?: number;
}

export interface EligibleCount {
  count: number;
  freeze_id?: string;
  frozen_as_of?: number;
  expires_at?: number;
}

export const giveawayDefaults: GiveawayConfig = {
  fields: {
    defaultPrize: '',
    winners: String(pins.giveawayDefaultWinners),
    windowSeconds: String(pins.giveawayEntryWindow),
    claimSeconds: String(pins.claimWindowDefault)
  },
  defaultPreset: '',
  presets: []
};

export function giveawayCountdown(deadline: number, now: number): string {
  const seconds = Math.max(0, Math.ceil((deadline - now) / 1000));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`;
}
