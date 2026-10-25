// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { namespaceReplyTemplate, type ChannelPointReward } from '@bagel/kit';
import { CHANNELPOINTS_MODULE } from '@bagel/kit/catalog/channelpoints';

export const REWARD_DEFAULT_COLOR = '#9147ff';
const COST_MAX = 10_000_000;
const COOLDOWN_MAX = 604_800;
const POINTS_MAX = 100_000_000;

export const normalizeCounterName = (value: string) => value.trim().replace(/^!/, '').toLowerCase().slice(0, 64);

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

const wholeAtLeast = (n: unknown, min: number, max = Number.MAX_SAFE_INTEGER) =>
  Number.isInteger(n) && (n as number) >= min && (n as number) <= max;

const invalidLimit = (enabled: boolean, value: unknown, max?: number) => enabled && !wholeAtLeast(value, 1, max);

function limitErrors(draft: ChannelPointReward): RewardErrors {
  const errors: RewardErrors = {};
  if (invalidLimit(draft.maxPerStreamEnabled, draft.maxPerStream)) errors.perStream = REWARD_ERROR_KEYS.perStream;
  if (invalidLimit(draft.maxPerUserPerStreamEnabled, draft.maxPerUserPerStream)) errors.perUser = REWARD_ERROR_KEYS.perUser;
  if (invalidLimit(draft.globalCooldownEnabled, draft.globalCooldownSeconds, COOLDOWN_MAX)) {
    errors.cooldown = REWARD_ERROR_KEYS.cooldown;
  }
  return errors;
}

export function rewardErrors(draft: ChannelPointReward, counterOn: boolean, pointsOn: boolean): RewardErrors {
  const errors: RewardErrors = limitErrors(draft);
  if (!draft.title.trim()) errors.title = REWARD_ERROR_KEYS.title;
  if (!wholeAtLeast(draft.cost, 1, COST_MAX)) errors.cost = REWARD_ERROR_KEYS.cost;
  if (counterOn && !normalizeCounterName(draft.counter)) errors.counter = REWARD_ERROR_KEYS.counter;
  if (pointsOn && !wholeAtLeast(draft.points, 1, POINTS_MAX)) errors.points = REWARD_ERROR_KEYS.points;
  return errors;
}
