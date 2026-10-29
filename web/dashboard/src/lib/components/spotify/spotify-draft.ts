// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { namespaceReplyTemplate, type SpotifyRedeemConfig } from '@bagel/kit';
import { SONGQUEUE_MODULE } from '@bagel/kit/catalog/songqueue';
import {
  rewardCoreErrors,
  rewardCoreFormFields,
  type RewardCoreDraft,
  type RewardCoreErrorField,
  type RewardCoreErrors
} from '#lib/reward-fields';

export type SpotifyRewardDraft = RewardCoreDraft;

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

export type SpotifyErrorField = RewardCoreErrorField;
export type SpotifyErrors = RewardCoreErrors;

export const SPOTIFY_ERROR_KEYS: Readonly<Record<SpotifyErrorField, string>> = Object.freeze({
  title: 'spotify.errors.titleRequired',
  cost: 'spotify.errors.cost',
  cooldown: 'spotify.errors.cooldown'
});

export const spotifyErrors = (draft: SpotifyRewardDraft): SpotifyErrors =>
  rewardCoreErrors(draft, SPOTIFY_ERROR_KEYS);

export const spotifyFormFields = (draft: SpotifyRewardDraft): Record<string, string> =>
  rewardCoreFormFields(draft, namespaceSpotifyReply);
