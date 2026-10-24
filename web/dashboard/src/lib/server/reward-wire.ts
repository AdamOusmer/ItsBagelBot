// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc, rpcReply } from '@bagel/kit/server/nats';
import { SUB, publishEventSubEnsureOptional } from './services';

export interface RewardWire {
  id?: string;
  title: string;
  cost: number;
  prompt?: string;
  background_color?: string;
  is_enabled: boolean;
  is_paused: boolean;
  is_user_input_required: boolean;
  should_skip_queue: boolean;
  max_per_stream_enabled: boolean;
  max_per_stream: number;
  max_per_user_per_stream_enabled: boolean;
  max_per_user_per_stream: number;
  global_cooldown_enabled: boolean;
  global_cooldown_seconds: number;
}

export interface RewardReplyWire {
  reward?: RewardWire;
  rewards?: RewardWire[];
  missing_scope?: boolean;
  not_affiliate?: boolean;
  error?: string;
  code?: string;
}

export interface MirroredReward {
  rewardId: string;
  title: string;
  cost: number;
  color: string;
  cooldown: number;
}

export type RewardRemoteResult = { ok: true } | { ok: false; missingScope?: boolean; error?: string };

const rewardSubject = (verb: string) => `${SUB.outgressRpc}.channelpoints.${verb}`;

export function callReward(verb: string, req: Record<string, unknown>): Promise<RewardReplyWire> {
  return rpcReply<RewardReplyWire>(rewardSubject(verb), req, 8000);
}

function callRewardOrThrow(userId: string, verb: string, req: Record<string, unknown>): Promise<RewardReplyWire> {
  return rpc<RewardReplyWire>(rewardSubject(verb), { broadcaster_id: userId, ...req }, 8000);
}

function rewardWire(
  draft: { title: string; cost: number; color: string; cooldown: number },
  id: string,
  prompt: string
): RewardWire {
  const cooldown = Number.isFinite(draft.cooldown) && draft.cooldown > 0 ? Math.trunc(draft.cooldown) : 0;
  return {
    id: id || undefined,
    title: draft.title,
    cost: draft.cost,
    prompt,
    background_color: draft.color || undefined,
    is_enabled: true,
    is_paused: false,
    is_user_input_required: true,
    should_skip_queue: false,
    max_per_stream_enabled: false,
    max_per_stream: 1,
    max_per_user_per_stream_enabled: false,
    max_per_user_per_stream: 1,
    global_cooldown_enabled: cooldown > 0,
    global_cooldown_seconds: cooldown
  };
}

function mirrorReward(reply: RewardWire, fallbackColor: string, existingId: string): MirroredReward {
  return {
    rewardId: reply.id ?? existingId,
    title: reply.title,
    cost: reply.cost,
    color: reply.background_color ?? fallbackColor,
    cooldown: reply.global_cooldown_enabled ? reply.global_cooldown_seconds : 0
  };
}

export interface RewardSave {
  draft: { title: string; cost: number; color: string; cooldown: number };
  prompt: string;
  existingId: string;
}

export async function saveRewardFlow(
  userId: string,
  { draft, prompt, existingId }: RewardSave,
  persist: (reward: MirroredReward) => Promise<void>
): Promise<RewardRemoteResult> {
  const verb = existingId ? 'update' : 'create';
  const req: Record<string, unknown> = { reward: rewardWire(draft, existingId, prompt) };
  if (existingId) req.reward_id = existingId;

  const reply = await callRewardOrThrow(userId, verb, req);
  if (reply.missing_scope) return { ok: false, missingScope: true };
  if (reply.error || !reply.reward) return { ok: false, error: reply.error ?? `${verb} failed` };

  await persist(mirrorReward(reply.reward, draft.color, existingId));
  if (!existingId) await publishEventSubEnsureOptional(userId);
  return { ok: true };
}

export async function deleteRemoteReward(userId: string, rewardId: string): Promise<RewardRemoteResult> {
  const reply = await callRewardOrThrow(userId, 'delete', { reward_id: rewardId });
  if (reply.missing_scope) return { ok: false, missingScope: true };
  if (reply.error) return { ok: false, error: reply.error };
  return { ok: true };
}
