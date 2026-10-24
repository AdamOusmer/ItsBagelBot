// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export const REWARD_TITLE_MAX = 45;
export const REWARD_COST_MIN = 1;
export const REWARD_COST_MAX = 10_000_000;
export const REWARD_COOLDOWN_MAX = 604_800;
export const REWARD_REPLY_MAX = 200;

const HEX_COLOR = /^#[0-9a-fA-F]{6}$/;

export const isHexColor = (value: string) => HEX_COLOR.test(value);

export const isValidRewardTitle = (title: string) => title.length > 0 && title.length <= REWARD_TITLE_MAX;

export const isValidRewardCost = (cost: number) =>
  Number.isFinite(cost) && cost >= REWARD_COST_MIN && cost <= REWARD_COST_MAX;

export const wholeBetween = (n: unknown, min: number, max = Number.MAX_SAFE_INTEGER) =>
  Number.isInteger(n) && (n as number) >= min && (n as number) <= max;

export interface RewardCoreDraft {
  title: string;
  cost: number;
  color: string;
  cooldown: number;
  onRedeem: string;
  replyMessage: string;
}

export type RewardCoreErrorField = 'title' | 'cost' | 'cooldown';
export type RewardCoreErrors = Partial<Record<RewardCoreErrorField, string>>;

export function rewardCoreErrors(
  draft: Pick<RewardCoreDraft, RewardCoreErrorField>,
  keys: Readonly<Record<RewardCoreErrorField, string>>
): RewardCoreErrors {
  const errors: RewardCoreErrors = {};
  if (!draft.title.trim()) errors.title = keys.title;
  if (!wholeBetween(draft.cost, REWARD_COST_MIN, REWARD_COST_MAX)) errors.cost = keys.cost;
  if (!wholeBetween(draft.cooldown, 0, REWARD_COOLDOWN_MAX)) errors.cooldown = keys.cooldown;
  return errors;
}

export function rewardCoreFormFields(
  draft: RewardCoreDraft,
  namespaceReply: (message: string) => string
): Record<string, string> {
  return {
    title: draft.title,
    cost: String(draft.cost),
    color: draft.color,
    cooldown: String(draft.cooldown),
    onRedeem: draft.onRedeem,
    replyMessage: namespaceReply(draft.replyMessage)
  };
}
