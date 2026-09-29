// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { moduleDef, namespaceReplyTemplate, type ChannelPointReward } from '@bagel/kit';

export const REWARD_DEFAULT_COLOR = '#9147ff';
const COST_MAX = 10_000_000;
const COOLDOWN_MAX = 604_800;

const reply = moduleDef('channelpoints')!.replies.find((r) => r.key === 'reply')!;

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

export type RewardErrorField = 'title' | 'cost' | 'counter' | 'perStream' | 'perUser' | 'cooldown';
export type RewardErrors = Partial<Record<RewardErrorField, string>>;

const wholeAtLeast = (n: unknown, min: number, max = Number.MAX_SAFE_INTEGER) =>
  Number.isInteger(n) && (n as number) >= min && (n as number) <= max;

const invalidLimit = (enabled: boolean, value: unknown, max?: number) => enabled && !wholeAtLeast(value, 1, max);

function limitErrors(draft: ChannelPointReward): RewardErrors {
  const errors: RewardErrors = {};
  if (invalidLimit(draft.maxPerStreamEnabled, draft.maxPerStream)) errors.perStream = 'channelpoints.errLimit';
  if (invalidLimit(draft.maxPerUserPerStreamEnabled, draft.maxPerUserPerStream)) errors.perUser = 'channelpoints.errLimit';
  if (invalidLimit(draft.globalCooldownEnabled, draft.globalCooldownSeconds, COOLDOWN_MAX)) {
    errors.cooldown = 'channelpoints.errCooldown';
  }
  return errors;
}

export function rewardErrors(draft: ChannelPointReward, counterOn: boolean): RewardErrors {
  const errors: RewardErrors = limitErrors(draft);
  if (!draft.title.trim()) errors.title = 'channelpoints.errTitleRequired';
  if (!wholeAtLeast(draft.cost, 1, COST_MAX)) errors.cost = 'channelpoints.errCost';
  if (counterOn && !draft.counter.trim()) errors.counter = 'rewardCounter.errNameRequired';
  return errors;
}
