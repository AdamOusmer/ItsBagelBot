// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc, rpcReply } from '@bagel/kit/server/nats';
import { POLICY } from '@bagel/kit/server/cache-keys';
import { type GoveeOnRedeem, type GoveeDevice, type GoveeReward, type GoveeBinding, MOD } from '@bagel/kit';
import { SUB, fabric, invalidate, publishEventSubEnsureOptional } from './services';
import { upsertModule } from './commands-store';
import { readModuleBlob, setModuleEnabled } from './module-blob';

export type { GoveeOnRedeem, GoveeDevice, GoveeReward, GoveeBinding };

const GOVEE_MODULE = MOD.govee;

const devicesCacheKey = (userId: string) => `govee-devices:${userId}`;

const REWARD_PROMPT = 'Type a colour: a name like blue, or a hex code like #00ccff';

export interface GoveeView {
  enabled: boolean;
  keyPresent: boolean;
  bindings: GoveeBinding[];
}

export interface RewardDraft {
  title: string;
  cost: number;
  onRedeem: GoveeOnRedeem;
  color: string;
  cooldown: number;
  replyMessage: string;
  allowOff: boolean;
  allowOffline: boolean;
}

export type GoveeResult = { ok: true } | { ok: false; missingScope?: boolean; error?: string };

function coerceOnRedeem(v: unknown): GoveeOnRedeem {
  return v === 'cancel' || v === 'leave' ? v : 'fulfill';
}

function readBinding(configs: unknown): GoveeBinding {
  const c = (configs ?? {}) as Partial<GoveeBinding>;
  const reward = c.reward && typeof c.reward === 'object' ? (c.reward as Partial<GoveeReward>) : null;
  return {
    device: String(c.device ?? ''),
    sku: String(c.sku ?? ''),
    deviceName: String(c.deviceName ?? ''),
    onRedeem: coerceOnRedeem(c.onRedeem),
    rewardId: String(c.rewardId ?? ''),
    reward: reward
      ? {
          rewardId: String(reward.rewardId ?? ''),
          title: String(reward.title ?? ''),
          cost: Number(reward.cost ?? 0),
          color: String(reward.color ?? ''),
          cooldown: Number(reward.cooldown ?? 0)
        }
      : null,
    allowOffline: c.allowOffline === true,
    allowOff: c.allowOff === true,
    replyMessage: String(c.replyMessage ?? '')
  };
}

function readBindings(configs: unknown): GoveeBinding[] {
  const c = (configs ?? {}) as { bindings?: unknown };
  if (Array.isArray(c.bindings)) {
    return c.bindings.map(readBinding).filter((b) => b.device);
  }
  const single = readBinding(configs);
  return single.device ? [single] : [];
}

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
  reward?: RewardWire & { id?: string };
  missing_scope?: boolean;
  error?: string;
}

function rewardWire(draft: RewardDraft, id: string): RewardWire {
  const cooldown = Number.isFinite(draft.cooldown) && draft.cooldown > 0 ? Math.trunc(draft.cooldown) : 0;
  return {
    id: id || undefined,
    title: draft.title,
    cost: draft.cost,
    prompt: REWARD_PROMPT,
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

function callReward(userId: string, verb: string, req: Record<string, unknown>): Promise<RewardReplyWire> {
  return rpc<RewardReplyWire>(`${SUB.outgressRpc}.channelpoints.${verb}`, { broadcaster_id: userId, ...req }, 8000);
}

function bindingFromReply(
  device: GoveeDevice,
  draft: RewardDraft,
  reply: NonNullable<RewardReplyWire['reward']>,
  existingId: string
): GoveeBinding {
  const rewardId = reply.id ?? existingId;
  const color = reply.background_color ?? draft.color;
  const cooldown = reply.global_cooldown_enabled ? reply.global_cooldown_seconds : 0;
  return {
    device: device.device,
    sku: device.sku,
    deviceName: device.name,
    onRedeem: draft.onRedeem,
    rewardId,
    reward: { rewardId, title: reply.title, cost: reply.cost, color, cooldown },
    allowOffline: draft.allowOffline,
    allowOff: draft.allowOff,
    replyMessage: draft.replyMessage
  };
}

export interface GoveeStore {
  read(): Promise<GoveeView>;
  setKey(key: string): Promise<GoveeResult>;
  clearKey(): Promise<GoveeResult>;
  listDevices(): Promise<{ devices: GoveeDevice[]; error?: string }>;
  setEnabled(enabled: boolean): Promise<GoveeResult>;
  saveReward(device: GoveeDevice, draft: RewardDraft): Promise<GoveeResult>;
  deleteReward(deviceId: string): Promise<GoveeResult>;
}

export function goveeStore(userId: string): GoveeStore {
  async function keyPresent(): Promise<boolean> {
    try {
      const r = await rpc<{ present?: boolean }>(`${SUB.goveeKey}.status`, { user_id: userId }, 3000);
      return !!r.present;
    } catch {
      return false;
    }
  }

  async function read(): Promise<GoveeView> {
    const [blob, present] = await Promise.all([
      readModuleBlob<unknown>(userId, GOVEE_MODULE),
      keyPresent()
    ]);
    return { enabled: blob.enabled, keyPresent: present, bindings: readBindings(blob.configs) };
  }

  async function writeBindings(enabled: boolean, bindings: GoveeBinding[]): Promise<void> {
    await upsertModule(userId, GOVEE_MODULE, enabled, { bindings } as unknown as Record<string, unknown>);
  }

  async function setKey(key: string): Promise<GoveeResult> {
    const r = await rpcReply<{ error?: string }>(`${SUB.goveeKey}.set`, { user_id: userId, key }, 3000);
    if (r.error) return { ok: false, error: r.error };
    invalidate(devicesCacheKey(userId));
    return { ok: true };
  }

  async function clearKey(): Promise<GoveeResult> {
    const r = await rpcReply<{ error?: string }>(`${SUB.goveeKey}.clear`, { user_id: userId }, 3000);
    if (r.error) return { ok: false, error: r.error };
    invalidate(devicesCacheKey(userId));
    return { ok: true };
  }

  async function listDevices(): Promise<{ devices: GoveeDevice[]; error?: string }> {
    try {
      const devices = await fabric.readKey(devicesCacheKey(userId), POLICY.govee, async () => {
        const r = await rpc<{ devices?: GoveeDevice[] }>(
          `${SUB.gossip}.govee.devices`,
          { channel_id: userId },
          // Just over gossip's 8 s devices handler budget.
          9000
        );
        return Array.isArray(r.devices) ? r.devices : [];
      });
      return { devices };
    } catch (e) {
      return { devices: [], error: e instanceof Error ? e.message : 'device lookup failed' };
    }
  }

  async function saveReward(device: GoveeDevice, draft: RewardDraft): Promise<GoveeResult> {
    const cur = await read();
    const existingId = cur.bindings.find((b) => b.device === device.device)?.rewardId ?? '';
    const verb = existingId ? 'update' : 'create';
    const req: Record<string, unknown> = { reward: rewardWire(draft, existingId) };
    if (existingId) req.reward_id = existingId;

    const reply = await callReward(userId, verb, req);
    if (reply.missing_scope) return { ok: false, missingScope: true };
    if (reply.error || !reply.reward) return { ok: false, error: reply.error ?? `${verb} failed` };

    const binding = bindingFromReply(device, draft, reply.reward, existingId);
    await writeBindings(cur.enabled, [...cur.bindings.filter((b) => b.device !== device.device), binding]);
    if (!existingId) await publishEventSubEnsureOptional(userId);
    return { ok: true };
  }

  async function deleteReward(deviceId: string): Promise<GoveeResult> {
    const cur = await read();
    const target = cur.bindings.find((b) => b.device === deviceId);
    if (!target) return { ok: true };
    if (target.rewardId) {
      const reply = await callReward(userId, 'delete', { reward_id: target.rewardId });
      if (reply.missing_scope) return { ok: false, missingScope: true };
      if (reply.error) return { ok: false, error: reply.error };
    }
    const bindings = cur.bindings.filter((b) => b.device !== deviceId);
    await writeBindings(cur.enabled, bindings);
    return { ok: true };
  }

  return {
    read,
    setKey,
    clearKey,
    listDevices,
    setEnabled: async (enabled: boolean) => {
      await setModuleEnabled(userId, GOVEE_MODULE, enabled);
      return { ok: true };
    },
    saveReward,
    deleteReward
  };
}
