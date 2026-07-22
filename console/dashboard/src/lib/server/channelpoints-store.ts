// Channel-points store: bridges the two homes of a reward.
//
//   - The Twitch custom reward itself is owned by outgress and mutated
//     synchronously over NATS RPC under the broadcaster's own token
//     (bagel.rpc.outgress.channelpoints.*). Only rewards our client id created
//     are manageable, so the dashboard is the create surface.
//   - The reward -> bot-action binding is stored in the hidden "channelpoints"
//     module blob (the same modules service every other feature uses) and read
//     by sesame's channelpoints module.
//
// The blob is the source of truth for what the tab renders (one fast read, no
// Twitch round trip per page load); Twitch is the source of truth for the
// reward's existence. Every mutation goes to Twitch first (where it must
// succeed) and then rewrites the blob, so the two never diverge on a failed
// call. After a create/enable we also fire an ensure-optional EventSub job so
// the channel starts receiving redemption events.
import { rpc } from '@bagel/shared/server/nats';
import { logger } from '@bagel/shared/server/logger';
import type { ChannelPointReward } from '@bagel/shared';
import { SUB, publishEventSubEnsureOptional } from './services';
import { listModules, upsertModule } from './commands-store';
import { createCounter } from './loyalty-store';

const CP_MODULE = 'channelpoints';

// RewardWire is the snake_case mirror of the Go manage.Reward RPC contract.
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
}

// A rewards write can fail two distinct ways the UI must tell apart: a plain
// failure (shown as an error toast) and a missing-scope rejection (shown as a
// reconnect CTA, because the broadcaster's grant predates the redemption scope).
export type RewardResult =
  | { ok: true; reward?: ChannelPointReward }
  | { ok: false; missingScope?: boolean; error?: string };

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
    // Our rewards always ride Twitch's request queue so the bot can resolve
    // (fulfill/cancel) them and the redemption.add event carries them as
    // UNFULFILLED. A skipped-queue redemption cannot be updated.
    should_skip_queue: false,
    max_per_stream_enabled: r.maxPerStreamEnabled,
    max_per_stream: r.maxPerStream,
    max_per_user_per_stream_enabled: r.maxPerUserPerStreamEnabled,
    max_per_user_per_stream: r.maxPerUserPerStream,
    global_cooldown_enabled: r.globalCooldownEnabled,
    global_cooldown_seconds: r.globalCooldownSeconds
  };
}

// mergeTwitch takes the Twitch-normalized reward outgress echoed back and keeps
// the local action binding from the draft (Twitch knows nothing about it).
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

// ensureRewardCounter creates the reward's bound counter with the chosen scope
// if it doesn't exist yet, so a broadcaster can make the counter straight from
// the reward editor. Best-effort: the reward binding is the authoritative save,
// so a loyalty-service blip (or the service not yet deployed) must not fail the
// reward write. Create is idempotent — an existing counter keeps its scope.
async function ensureRewardCounter(userId: string, reward: ChannelPointReward): Promise<void> {
  if (!reward.counter) return;
  try {
    await createCounter(userId, reward.counter, reward.counterScope);
  } catch (e) {
    logger.error({ err: e }, '[channelpoints] ensure counter failed');
  }
}

// readRewards loads the current bindings blob (enable flag + reward records).
export async function readRewards(userId: string): Promise<RewardsView> {
  const rows = await listModules(userId);
  const row = rows.find((r) => r.name === CP_MODULE);
  const enabled = row ? row.is_enabled : false;
  const configs = (row?.configs ?? {}) as { rewards?: ChannelPointReward[] };
  return { enabled, rewards: Array.isArray(configs.rewards) ? configs.rewards : [] };
}

async function writeRewards(userId: string, enabled: boolean, rewards: ChannelPointReward[]): Promise<void> {
  await upsertModule(userId, CP_MODULE, enabled, rewards.length ? { rewards } : {});
}

async function callReward(verb: string, req: Record<string, unknown>): Promise<RewardReplyWire> {
  return rpc<RewardReplyWire>(`${SUB.outgressRpc}.channelpoints.${verb}`, req, 8000);
}

export async function createReward(userId: string, draft: ChannelPointReward): Promise<RewardResult> {
  const reply = await callReward('create', { broadcaster_id: userId, reward: toWire(draft) });
  if (reply.missing_scope) return { ok: false, missingScope: true };
  if (reply.error || !reply.reward) return { ok: false, error: reply.error ?? 'create failed' };

  const created = mergeTwitch(reply.reward, draft);
  const cur = await readRewards(userId);
  // Adding the first reward turns the module on so redemptions are acted on;
  // later adds preserve whatever enable state the broadcaster set.
  const enabled = cur.rewards.length === 0 ? true : cur.enabled;
  await writeRewards(userId, enabled, [...cur.rewards, created]);
  await ensureRewardCounter(userId, created);
  await publishEventSubEnsureOptional(userId);
  return { ok: true, reward: created };
}

export async function updateReward(userId: string, draft: ChannelPointReward): Promise<RewardResult> {
  if (!draft.id) return { ok: false, error: 'missing reward id' };
  const reply = await callReward('update', { broadcaster_id: userId, reward_id: draft.id, reward: toWire(draft) });
  if (reply.missing_scope) return { ok: false, missingScope: true };
  if (reply.error || !reply.reward) return { ok: false, error: reply.error ?? 'update failed' };

  const updated = mergeTwitch(reply.reward, draft);
  const cur = await readRewards(userId);
  const rewards = cur.rewards.map((r) => (r.id === draft.id ? updated : r));
  await writeRewards(userId, cur.enabled, rewards);
  await ensureRewardCounter(userId, updated);
  return { ok: true, reward: updated };
}

export async function deleteReward(userId: string, rewardId: string): Promise<RewardResult> {
  const reply = await callReward('delete', { broadcaster_id: userId, reward_id: rewardId });
  if (reply.missing_scope) return { ok: false, missingScope: true };
  if (reply.error) return { ok: false, error: reply.error };

  const cur = await readRewards(userId);
  await writeRewards(userId, cur.enabled, cur.rewards.filter((r) => r.id !== rewardId));
  return { ok: true };
}

// setEnabled flips the whole module on/off (whether sesame acts on redemptions
// at all) without touching the rewards themselves.
export async function setChannelPointsEnabled(userId: string, enabled: boolean): Promise<void> {
  const cur = await readRewards(userId);
  await writeRewards(userId, enabled, cur.rewards);
}
