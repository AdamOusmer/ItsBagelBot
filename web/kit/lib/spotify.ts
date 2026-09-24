// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RewardOnRedeem } from './types';

export type { RewardOnRedeem };

export const SPOTIFY_SR_PERMS = ['everyone', 'sub', 'vip', 'mod', 'broadcaster'] as const;
export type SpotifySrPerm = (typeof SPOTIFY_SR_PERMS)[number];

export interface SpotifySrConfig {
  enabled: boolean;
  perm: SpotifySrPerm;
  allowOffline: boolean;
}

export interface SpotifyReward {
  rewardId: string;
  title: string;
  cost: number;
  color: string;
  cooldown: number;
}

export interface SpotifyRedeemConfig {
  enabled: boolean;
  rewardId: string;
  onRedeem: RewardOnRedeem;
  replyMessage: string;
  reward: SpotifyReward | null;
  allowOffline: boolean;
}

export function blankSpotifySr(): SpotifySrConfig {
  return { enabled: false, perm: 'everyone', allowOffline: false };
}

export function blankSpotifyRedeem(): SpotifyRedeemConfig {
  return {
    enabled: false,
    rewardId: '',
    onRedeem: 'fulfill',
    replyMessage: '',
    reward: null,
    allowOffline: false
  };
}

export function scopeGap(required: readonly string[], granted: readonly string[]): string[] {
  const have = new Set(granted);
  return required.filter((scope) => !have.has(scope));
}

export interface SpotifyQuotas {
  everyone: number | null;
  sub: number | null;
  vip: number | null;
  mod: number | null;
}

export const SPOTIFY_QUOTA_TIERS = ['everyone', 'sub', 'vip', 'mod'] as const;
export type SpotifyQuotaTier = (typeof SPOTIFY_QUOTA_TIERS)[number];

export function blankSpotifyQuotas(): SpotifyQuotas {
  return { everyone: null, sub: null, vip: null, mod: null };
}
