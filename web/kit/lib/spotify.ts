// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Song requests (Spotify) shapes, shared by the server store and the dashboard
// components. The sr/redeem halves live inside the "songqueue" module blob:
// written by the dashboard's songqueue page, read by sesame's songqueue module
// (app/twitch/sesame/modules/songqueue.go). The perm strings are exactly what the
// engine's ParsePerm accepts, so the command gate reads them straight from the
// blob with no translation.
import type { RewardOnRedeem } from './types';

export type { RewardOnRedeem };

// Who may queue a song via !sr, widest to narrowest.
export const SPOTIFY_SR_PERMS = ['everyone', 'sub', 'vip', 'mod', 'broadcaster'] as const;
export type SpotifySrPerm = (typeof SPOTIFY_SR_PERMS)[number];

// SpotifySrConfig is the chat-command half: on/off plus the permission tier.
// allowOffline opts out of the live-only gate, exactly like govee's: the
// default (false) means requests are only taken while the stream is up, which
// is what a broadcaster expects from a queue they have to play through.
export interface SpotifySrConfig {
  enabled: boolean;
  perm: SpotifySrPerm;
  allowOffline: boolean;
}

// SpotifyReward mirrors the Twitch reward settings shown for song requests.
export interface SpotifyReward {
  rewardId: string;
  title: string;
  cost: number;
  // Tile background as "#rrggbb"; '' means Twitch's default.
  color: string;
  // Global cooldown in seconds; 0 disables.
  cooldown: number;
}

// SpotifyRedeemConfig is the channel-points half: the bound reward plus what
// happens when it redeems.
export interface SpotifyRedeemConfig {
  enabled: boolean;
  rewardId: string;
  onRedeem: RewardOnRedeem;
  replyMessage: string;
  reward: SpotifyReward | null;
  // Same live gate as the chat path, tracked separately: a channel can take
  // points requests around the clock while keeping !sr live-only, or vice
  // versa. Points spent while the gate is closed are refunded, never eaten.
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

/**
 * scopeGap is what a stored Spotify grant is missing against what the connect
 * flow asks for today, in the order the flow asks for them. The dashboard
 * owns the "asks for" half (it comes from env); this owns what short means.
 *
 * An empty granted list is treated as missing everything on purpose: it means
 * a grant recorded before custody kept scopes, and assuming those older grants
 * are complete is exactly what leaves a broadcaster hitting 403s on !skip with
 * nothing in the console saying why.
 */
export function scopeGap(required: readonly string[], granted: readonly string[]): string[] {
  const have = new Set(granted);
  return required.filter((scope) => !have.has(scope));
}

// SpotifyQuotas is the per-tier pending-request cap sesame enforces on adds.
// null means UNLIMITED for that tier, which is also every tier's default. The
// broadcaster is never capped, so they have no field here.
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
