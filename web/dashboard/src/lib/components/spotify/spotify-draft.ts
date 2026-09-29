// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { moduleDef, namespaceReplyTemplate, type SpotifyRedeemConfig } from '@bagel/kit';

export interface SpotifyRewardDraft {
  title: string;
  cost: number;
  color: string;
  cooldown: number;
  onRedeem: string;
  replyMessage: string;
}

const COST_MAX = 10_000_000;
export const SPOTIFY_COOLDOWN_MAX = 604_800;

const reply = moduleDef('songqueue')!.replies.find((r) => r.key === 'redeem')!;

export function namespaceSpotifyReply(message: string): string {
  return namespaceReplyTemplate('songqueue', reply, message);
}

export function spotifyDraftFor(redeem: SpotifyRedeemConfig, defaultTitle: string): SpotifyRewardDraft {
  return {
    title: redeem.reward?.title || defaultTitle,
    cost: redeem.reward?.cost ?? 500,
    color: redeem.reward?.color || '#1db954',
    cooldown: redeem.reward?.cooldown ?? 0,
    onRedeem: redeem.onRedeem ?? 'fulfill',
    replyMessage: namespaceSpotifyReply(redeem.replyMessage ?? '')
  };
}

export type SpotifyErrorField = 'title' | 'cost' | 'cooldown';
export type SpotifyErrors = Partial<Record<SpotifyErrorField, string>>;

const whole = (n: unknown, min: number, max: number) =>
  Number.isInteger(n) && (n as number) >= min && (n as number) <= max;

export function spotifyErrors(draft: SpotifyRewardDraft): SpotifyErrors {
  const errors: SpotifyErrors = {};
  if (!draft.title.trim()) errors.title = 'spotify.errTitleRequired';
  if (!whole(draft.cost, 1, COST_MAX)) errors.cost = 'spotify.errCost';
  if (!whole(draft.cooldown, 0, SPOTIFY_COOLDOWN_MAX)) errors.cooldown = 'spotify.errCooldown';
  return errors;
}

export function spotifyFormFields(draft: SpotifyRewardDraft): Record<string, string> {
  return {
    title: draft.title,
    cost: String(draft.cost),
    color: draft.color,
    cooldown: String(draft.cooldown),
    onRedeem: draft.onRedeem,
    replyMessage: namespaceSpotifyReply(draft.replyMessage)
  };
}
