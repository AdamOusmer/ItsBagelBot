// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { isSetupValue, setupGaps, type Definition, type Issue, type Params } from '@bagel/kit/automations';
import { REWARD_COOLDOWN_MAX, isHexColor, isValidRewardCost, isValidRewardTitle, wholeBetween } from '#lib/reward-fields';
import { callReward, deleteRemoteReward, saveRewardFlow, type MirroredReward, type RewardRemoteResult } from './reward-wire';

type UserId = string;

export interface ManagedReward {
  rewardId: string;
  title: string;
  cost: number;
  color: string;
  cooldown: number;
  inputRequired: boolean;
}

export type RewardSync = { ok: true; definition: Definition } | { ok: false; error: string };

export function managedReward(definition: Definition): ManagedReward | null {
  const { trigger } = definition;
  const params: Params = trigger.params ?? {};
  const title = trigger.type === 'reward' && typeof params.title === 'string' ? params.title : '';
  if (title === '') return null;
  return {
    rewardId: String(params.reward_id ?? ''),
    title,
    cost: Number(params.cost),
    color: String(params.color ?? ''),
    cooldown: Number(params.cooldown_s ?? 0),
    inputRequired: params.input_required === true
  };
}

const existingId = (reward: ManagedReward) => (isSetupValue(reward.rewardId) ? '' : reward.rewardId);

const failure = (result: Extract<RewardRemoteResult, { ok: false }>): { ok: false; error: string } => ({
  ok: false,
  error: result.missingScope ? 'missing_scope' : (result.error ?? 'reward_failed')
});

function withReward(definition: Definition, mirrored: MirroredReward): Definition {
  const params = { ...definition.trigger.params, reward_id: mirrored.rewardId, title: mirrored.title, cost: mirrored.cost, color: mirrored.color, cooldown_s: mirrored.cooldown };
  return { ...definition, trigger: { ...definition.trigger, params } };
}

type Titled = { title: string };

const sameTitle = (left: Titled, right: Titled) => left.title.trim().toLowerCase() === right.title.trim().toLowerCase();

async function adoptableId(userId: UserId, reward: ManagedReward): Promise<string> {
  const known = existingId(reward);
  if (known !== '') return known;
  const { rewards = [] } = await callReward('list', { broadcaster_id: userId });
  return rewards.find((listed) => listed.id && sameTitle(listed, reward))?.id ?? '';
}

async function pushReward(userId: UserId, reward: ManagedReward, enabled: boolean): Promise<{ result: RewardRemoteResult; mirrored?: MirroredReward }> {
  let mirrored: MirroredReward | undefined;
  const flags = { enabled, userInput: reward.inputRequired };
  const result = await saveRewardFlow(userId, { draft: { ...reward }, prompt: reward.title, existingId: await adoptableId(userId, reward), flags }, async (reply) => {
    mirrored = reply;
  });
  return { result, mirrored };
}

export async function syncReward(userId: UserId, definition: Definition, enabled: boolean): Promise<RewardSync> {
  const reward = managedReward(definition);
  if (!reward) return { ok: true, definition };
  const { result, mirrored } = await pushReward(userId, reward, enabled);
  if (!result.ok) return failure(result);
  return { ok: true, definition: withReward(definition, mirrored!) };
}

export async function setRewardEnabled(userId: UserId, definition: Definition, enabled: boolean): Promise<RewardSync> {
  const reward = managedReward(definition);
  if (!reward || existingId(reward) === '') return { ok: true, definition };
  const { result } = await pushReward(userId, reward, enabled);
  return result.ok ? { ok: true, definition } : failure(result);
}

export async function deleteManagedReward(userId: UserId, definition: Definition): Promise<{ ok: true } | { ok: false; error: string }> {
  const reward = managedReward(definition);
  if (!reward || existingId(reward) === '') return { ok: true };
  const result = await deleteRemoteReward(userId, reward.rewardId);
  return result.ok ? { ok: true } : failure(result);
}

export type RewardSaved = { ok: true; reward: MirroredReward } | { ok: false; error: string };

export async function saveManagedReward(userId: UserId, reward: ManagedReward): Promise<RewardSaved> {
  const { result, mirrored } = await pushReward(userId, reward, true);
  return result.ok ? { ok: true, reward: mirrored! } : failure(result);
}

type DraftJson = string;

function parseJson(text: DraftJson): Record<string, unknown> | null {
  try {
    const body: unknown = JSON.parse(text);
    return typeof body === 'object' && body !== null && !Array.isArray(body) ? (body as Record<string, unknown>) : null;
  } catch {
    return null;
  }
}

type RewardFields = Pick<ManagedReward, 'title' | 'cost' | 'cooldown' | 'color'>;

const validColor = (fields: RewardFields) => fields.color === '' || isHexColor(fields.color);

const validRewardFields = (fields: RewardFields) =>
  isValidRewardTitle(fields.title) && isValidRewardCost(fields.cost) && wholeBetween(fields.cooldown, 0, REWARD_COOLDOWN_MAX) && validColor(fields);

export function parseRewardDraft(text: DraftJson): ManagedReward | null {
  const body = parseJson(text);
  if (!body) return null;
  const title = typeof body.title === 'string' ? body.title.trim() : '';
  const color = typeof body.color === 'string' ? body.color : '';
  const cost = Number(body.cost);
  const cooldown = Number(body.cooldown ?? 0);
  if (!validRewardFields({ title, cost, cooldown, color })) return null;
  return { rewardId: typeof body.rewardId === 'string' ? body.rewardId : '', title, cost, color, cooldown, inputRequired: body.inputRequired === true };
}

const CREATED_ON_SAVE = 'created-on-save';

export function gapsBeforeSync(definition: Definition): Issue[] {
  const reward = managedReward(definition);
  if (!reward || existingId(reward) !== '') return setupGaps(definition);
  return setupGaps({ ...definition, trigger: { ...definition.trigger, params: { ...definition.trigger.params, reward_id: CREATED_ON_SAVE } } });
}
