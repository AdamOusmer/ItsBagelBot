// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { namespaceReplyTemplate, type SpotifyRedeemConfig } from '@bagel/kit';
import { SONGQUEUE_MODULE } from '@bagel/kit/catalog/songqueue';

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

const reply = SONGQUEUE_MODULE.replies.find((r) => r.key === 'redeem')!;

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

export const SPOTIFY_ERROR_KEYS: Readonly<Record<SpotifyErrorField, string>> = Object.freeze({
  title: 'spotify.errors.titleRequired',
  cost: 'spotify.errors.cost',
  cooldown: 'spotify.errors.cooldown'
});

const whole = (n: unknown, min: number, max: number) =>
  Number.isInteger(n) && (n as number) >= min && (n as number) <= max;

export function spotifyErrors(draft: SpotifyRewardDraft): SpotifyErrors {
  const errors: SpotifyErrors = {};
  if (!draft.title.trim()) errors.title = SPOTIFY_ERROR_KEYS.title;
  if (!whole(draft.cost, 1, COST_MAX)) errors.cost = SPOTIFY_ERROR_KEYS.cost;
  if (!whole(draft.cooldown, 0, SPOTIFY_COOLDOWN_MAX)) errors.cooldown = SPOTIFY_ERROR_KEYS.cooldown;
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
