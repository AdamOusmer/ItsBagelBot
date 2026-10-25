// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { namespaceReplyTemplate, type ChannelPointReward } from '@bagel/kit';
import { CHANNELPOINTS_MODULE } from '@bagel/kit/catalog/channelpoints';
import { REWARD_COOLDOWN_MAX, REWARD_COST_MAX, normalizeCounterName, wholeBetween } from '$lib/reward-fields';

export const REWARD_DEFAULT_COLOR = '#9147ff';

const reply = CHANNELPOINTS_MODULE.replies.find((r) => r.key === 'reply')!;

export function namespaceRewardMessage(message: string): string {
  return namespaceReplyTemplate('channelpoints', reply, message);
}

export function rewardDraftFrom(reward: ChannelPointReward): ChannelPointReward {
  return {
    ...reward,
    backgroundColor: reward.backgroundColor || REWARD_DEFAULT_COLOR,
    message: namespaceRewardMessage(reward.message)
  };
}

export type RewardErrorField = 'title' | 'cost' | 'counter' | 'points' | 'perStream' | 'perUser' | 'cooldown';
export type RewardErrors = Partial<Record<RewardErrorField, string>>;

export const REWARD_ERROR_KEYS: Readonly<Record<RewardErrorField, string>> = Object.freeze({
  perStream: 'channelpoints.errLimit',
  perUser: 'channelpoints.errLimit',
  cooldown: 'channelpoints.errCooldown',
  title: 'channelpoints.errTitleRequired',
  cost: 'channelpoints.errCost',
  counter: 'rewardCounter.errNameRequired',
  points: 'channelpoints.errCost'
});

const REWARD_POINTS_MAX = 100_000_000;

const invalidLimit = (enabled: boolean, value: unknown, max?: number) => enabled && !wholeBetween(value, 1, max);

function limitErrors(draft: ChannelPointReward): RewardErrors {
  const errors: RewardErrors = {};
  if (invalidLimit(draft.maxPerStreamEnabled, draft.maxPerStream)) errors.perStream = REWARD_ERROR_KEYS.perStream;
  if (invalidLimit(draft.maxPerUserPerStreamEnabled, draft.maxPerUserPerStream)) errors.perUser = REWARD_ERROR_KEYS.perUser;
  if (invalidLimit(draft.globalCooldownEnabled, draft.globalCooldownSeconds, REWARD_COOLDOWN_MAX)) {
    errors.cooldown = REWARD_ERROR_KEYS.cooldown;
  }
  return errors;
}

export function rewardErrors(draft: ChannelPointReward, counterOn: boolean, pointsOn: boolean): RewardErrors {
  const errors: RewardErrors = limitErrors(draft);
  if (!draft.title.trim()) errors.title = REWARD_ERROR_KEYS.title;
  if (!wholeBetween(draft.cost, 1, REWARD_COST_MAX)) errors.cost = REWARD_ERROR_KEYS.cost;
  if (counterOn && !normalizeCounterName(draft.counter)) errors.counter = REWARD_ERROR_KEYS.counter;
  if (pointsOn && !wholeBetween(draft.points, 1, REWARD_POINTS_MAX)) errors.points = REWARD_ERROR_KEYS.points;
  return errors;
}
