// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Twitch first, then the binding blob, so the two never diverge on a failed call.
import { rpcRefusal, rpcReply } from '@bagel/kit/server/nats';
import { codeReader } from '@bagel/kit/server/rpc-code';
import { logger } from '@bagel/kit/server/logger';
import { type ChannelPointReward, MOD } from '@bagel/kit';
import { SUB, publishEventSubEnsureOptional } from './services';
import { upsertModule } from './commands-store';
import { readModuleBlob, setModuleEnabled } from './module-blob';
import { createCounter } from './loyalty-store';

const CP_MODULE = MOD.channelpoints;

interface RewardWire {
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

interface RewardReplyWire {
  reward?: RewardWire;
  rewards?: RewardWire[];
  missing_scope?: boolean;
  error?: string;
  code?: string;
}

export type RewardResult =
  | { ok: true; reward?: ChannelPointReward }
  | { ok: false; missingScope?: boolean; duplicateTitle?: boolean; error?: string };

type RewardFailure = Extract<RewardResult, { ok: false }>;

const replyCode = codeReader(['conflict'] as const);

export interface RewardsView {
  enabled: boolean;
  rewards: ChannelPointReward[];
}

function toWire(r: ChannelPointReward): RewardWire {
  return {
    id: r.id || undefined,
    title: r.title,
    cost: r.cost,
    prompt: r.prompt || undefined,
    background_color: r.backgroundColor || undefined,
    is_enabled: r.isEnabled,
    is_paused: r.isPaused,
    is_user_input_required: r.isUserInputRequired,
      // Always queued: a skipped-queue redemption can never be fulfilled or refunded.
    should_skip_queue: false,
    max_per_stream_enabled: r.maxPerStreamEnabled,
    max_per_stream: r.maxPerStream,
    max_per_user_per_stream_enabled: r.maxPerUserPerStreamEnabled,
    max_per_user_per_stream: r.maxPerUserPerStream,
    global_cooldown_enabled: r.globalCooldownEnabled,
    global_cooldown_seconds: r.globalCooldownSeconds
  };
}

function mergeTwitch(tw: RewardWire, local: ChannelPointReward): ChannelPointReward {
  return {
    id: tw.id ?? local.id,
    title: tw.title,
    cost: tw.cost,
    prompt: tw.prompt ?? '',
    backgroundColor: tw.background_color ?? '',
    isEnabled: tw.is_enabled,
    isPaused: tw.is_paused,
    isUserInputRequired: tw.is_user_input_required,
    maxPerStreamEnabled: tw.max_per_stream_enabled,
    maxPerStream: tw.max_per_stream,
    maxPerUserPerStreamEnabled: tw.max_per_user_per_stream_enabled,
    maxPerUserPerStream: tw.max_per_user_per_stream,
    globalCooldownEnabled: tw.global_cooldown_enabled,
    globalCooldownSeconds: tw.global_cooldown_seconds,
    action: local.action,
    message: local.message,
    onRedeem: local.onRedeem,
    counter: local.counter,
    counterScope: local.counterScope,
    points: local.points,
    liveOnly: local.liveOnly
  };
}

async function ensureRewardCounter(userId: string, reward: ChannelPointReward): Promise<void> {
  if (!reward.counter) return;
  try {
    await createCounter(userId, reward.counter, reward.counterScope);
  } catch (e) {
    logger.error({ err: e }, '[channelpoints] ensure counter failed');
  }
}

export async function readRewards(userId: string): Promise<RewardsView> {
  const { enabled, configs } = await readModuleBlob<{ rewards?: ChannelPointReward[] }>(userId, CP_MODULE);
  return { enabled, rewards: Array.isArray(configs.rewards) ? configs.rewards : [] };
}

async function writeRewards(userId: string, enabled: boolean, rewards: ChannelPointReward[]): Promise<void> {
  await upsertModule(userId, CP_MODULE, enabled, rewards.length ? { rewards } : {});
}

async function callReward(verb: string, req: Record<string, unknown>): Promise<RewardReplyWire> {
  return rpcReply<RewardReplyWire>(`${SUB.outgressRpc}.channelpoints.${verb}`, req, 8000);
}

function refusal(reply: RewardReplyWire): RewardFailure | null {
  if (reply.missing_scope) return { ok: false, missingScope: true };
  if (replyCode(reply) === 'conflict') return { ok: false, duplicateTitle: true };
  const err = rpcRefusal(reply);
  if (err) throw err;
  return null;
}

export async function createReward(userId: string, draft: ChannelPointReward): Promise<RewardResult> {
  const reply = await callReward('create', { broadcaster_id: userId, reward: toWire(draft) });
  const refused = refusal(reply);
  if (refused) return refused;
  if (!reply.reward) return { ok: false, error: 'create failed' };

  const created = mergeTwitch(reply.reward, draft);
  const cur = await readRewards(userId);
  const enabled = cur.rewards.length === 0 ? true : cur.enabled;
  await writeRewards(userId, enabled, [...cur.rewards, created]);
  await ensureRewardCounter(userId, created);
  await publishEventSubEnsureOptional(userId);
  return { ok: true, reward: created };
}

export async function updateReward(userId: string, draft: ChannelPointReward): Promise<RewardResult> {
  if (!draft.id) return { ok: false, error: 'missing reward id' };
  const reply = await callReward('update', { broadcaster_id: userId, reward_id: draft.id, reward: toWire(draft) });
  const refused = refusal(reply);
  if (refused) return refused;
  if (!reply.reward) return { ok: false, error: 'update failed' };

  const updated = mergeTwitch(reply.reward, draft);
  const cur = await readRewards(userId);
  const rewards = cur.rewards.map((r) => (r.id === draft.id ? updated : r));
  await writeRewards(userId, cur.enabled, rewards);
  await ensureRewardCounter(userId, updated);
  return { ok: true, reward: updated };
}

export async function deleteReward(userId: string, rewardId: string): Promise<RewardResult> {
  const refused = refusal(await callReward('delete', { broadcaster_id: userId, reward_id: rewardId }));
  if (refused) return refused;

  const cur = await readRewards(userId);
  await writeRewards(userId, cur.enabled, cur.rewards.filter((r) => r.id !== rewardId));
  return { ok: true };
}

export async function setChannelPointsEnabled(userId: string, enabled: boolean): Promise<void> {
  await setModuleEnabled(userId, CP_MODULE, enabled);
}
