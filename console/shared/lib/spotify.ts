// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Song requests (Spotify) shapes, shared by the server store and the dashboard
// components. The sr/redeem halves live inside the "songqueue" module blob:
// written by the dashboard's songqueue page, read by sesame's songqueue module
// (app/sesame/modules/songqueue.go). The perm strings are exactly what the
// engine's ParsePerm accepts, so the command gate reads them straight from the
// blob with no translation.
import type { RewardOnRedeem } from './types';

export type { RewardOnRedeem };

// Who may queue a song via !sr, widest to narrowest.
export const SPOTIFY_SR_PERMS = ['everyone', 'sub', 'vip', 'mod', 'broadcaster'] as const;
export type SpotifySrPerm = (typeof SPOTIFY_SR_PERMS)[number];

// SpotifySrConfig is the chat-command half: on/off plus the permission tier.
export interface SpotifySrConfig {
  enabled: boolean;
  perm: SpotifySrPerm;
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
}

export function blankSpotifySr(): SpotifySrConfig {
  return { enabled: false, perm: 'everyone' };
}

export function blankSpotifyRedeem(): SpotifyRedeemConfig {
  return { enabled: false, rewardId: '', onRedeem: 'fulfill', replyMessage: '', reward: null };
}
