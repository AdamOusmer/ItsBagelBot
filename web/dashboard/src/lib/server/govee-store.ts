// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc, rpcReply } from '@bagel/kit/server/nats';
import { POLICY } from '@bagel/kit/server/cache-keys';
import { type GoveeOnRedeem, type GoveeDevice, type GoveeScene, type GoveeReward, type GoveeBinding, MOD } from '@bagel/kit';
import { SUB, fabric, invalidate } from './services';
import { upsertModule } from './commands-store';
import { readModuleBlob, setModuleEnabled } from './module-blob';
import { deleteRemoteReward, saveRewardFlow, type MirroredReward } from './reward-wire';
import type { RewardCore } from './reward-form';

export type { GoveeOnRedeem, GoveeDevice, GoveeScene, GoveeReward, GoveeBinding };

const GOVEE_MODULE = MOD.govee;

const devicesCacheKey = (userId: string) => `govee-devices:${userId}`;

const scenesCachePrefix = (userId: string) => `govee-scenes:${userId}`;
const scenesCacheKey = (userId: string, device: string, sku: string) => `${scenesCachePrefix(userId)}:${sku}:${device}`;

const REWARD_PROMPT = 'Type a colour: a name like blue, or a hex code like #00ccff';

export interface GoveeView {
  enabled: boolean;
  keyPresent: boolean;
  bindings: GoveeBinding[];
}

export interface RewardDraft extends RewardCore {
  allowOff: boolean;
  allowOffline: boolean;
}

export type GoveeResult =
  | { ok: true }
  | { ok: false; missingScope?: boolean; error?: string; code?: 'key_invalid' | 'key_unchecked' };

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

function bindingFromReply(device: GoveeDevice, draft: RewardDraft, reward: MirroredReward): GoveeBinding {
  return {
    device: device.device,
    sku: device.sku,
    deviceName: device.name,
    onRedeem: draft.onRedeem,
    rewardId: reward.rewardId,
    reward,
    allowOffline: draft.allowOffline,
    allowOff: draft.allowOff,
    replyMessage: draft.replyMessage
  };
}

export interface GoveeScenesResult {
  scenes: GoveeScene[];
  error?: string;
  code?: 'key_rejected' | 'unreachable';
}

export interface GoveeStore {
  read(): Promise<GoveeView>;
  setKey(key: string): Promise<GoveeResult>;
  clearKey(): Promise<GoveeResult>;
  listDevices(): Promise<{ devices: GoveeDevice[]; error?: string; code?: 'key_rejected' | 'unreachable' }>;
  listScenes(device: string, sku: string): Promise<GoveeScenesResult>;
  setEnabled(enabled: boolean): Promise<GoveeResult>;
  saveReward(device: GoveeDevice, draft: RewardDraft): Promise<GoveeResult>;
  deleteReward(deviceId: string): Promise<GoveeResult>;
}

function lookupFailure(e: unknown, fallback: string): { error: string; code: 'key_rejected' | 'unreachable' } {
  return { error: e instanceof Error ? e.message : fallback, code: (e as { keyRejected?: boolean } | null)?.keyRejected === true ? 'key_rejected' : 'unreachable' };
}

function deviceLookupFailure(e: unknown): { devices: GoveeDevice[]; error: string; code: 'key_rejected' | 'unreachable' } {
  return { devices: [], ...lookupFailure(e, 'device lookup failed') };
}

type GoveeListReply = { key_rejected?: boolean; error?: string } & Record<string, unknown>;

// Just over gossip's 8 s handler budget.
const GOVEE_LIST_TIMEOUT_MS = 9000;

async function cachedList<T>(cacheKey: string, route: string, field: string, payload: Record<string, string>): Promise<T[]> {
  return fabric.readKey(cacheKey, POLICY.govee, async () => {
    const r = await rpcReply<GoveeListReply>(`${SUB.gossip}.govee.${route}`, payload, GOVEE_LIST_TIMEOUT_MS);
    if (r.key_rejected === true) throw Object.assign(new Error(r.error ?? 'key rejected'), { keyRejected: true });
    if (r.error) throw new Error(r.error);
    const list = r[field];
    return Array.isArray(list) ? (list as T[]) : [];
  });
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
    invalidate(devicesCacheKey(userId), scenesCachePrefix(userId));
    const status = await keyWorks();
    if (status === 'rejected') {
      await rpcReply(`${SUB.goveeKey}.clear`, { user_id: userId }, 3000).catch(() => undefined);
      return { ok: false, code: 'key_invalid' };
    }
    if (status === 'unreachable') return { ok: false, code: 'key_unchecked' };
    return { ok: true };
  }

  async function keyWorks(): Promise<'ok' | 'rejected' | 'unreachable'> {
    try {
      const r = await rpcReply<{ devices?: GoveeDevice[]; key_rejected?: boolean }>(
        `${SUB.gossip}.govee.devices`,
        { channel_id: userId, fresh: true },
        9000
      );
      if (Array.isArray(r.devices)) return 'ok';
      return r.key_rejected === true ? 'rejected' : 'unreachable';
    } catch {
      return 'unreachable';
    }
  }

  async function clearKey(): Promise<GoveeResult> {
    const r = await rpcReply<{ error?: string }>(`${SUB.goveeKey}.clear`, { user_id: userId }, 3000);
    if (r.error) return { ok: false, error: r.error };
    invalidate(devicesCacheKey(userId), scenesCachePrefix(userId));
    return { ok: true };
  }

  async function listDevices(): Promise<{ devices: GoveeDevice[]; error?: string; code?: 'key_rejected' | 'unreachable' }> {
    try {
      const devices = await cachedList<GoveeDevice>(devicesCacheKey(userId), 'devices', 'devices', { channel_id: userId });
      return { devices };
    } catch (e) {
      return deviceLookupFailure(e);
    }
  }

  async function listScenes(device: string, sku: string): Promise<GoveeScenesResult> {
    try {
      const scenes = await cachedList<GoveeScene>(scenesCacheKey(userId, device, sku), 'scenes', 'scenes', {
        channel_id: userId,
        device,
        sku
      });
      return { scenes };
    } catch (e) {
      return { scenes: [], ...lookupFailure(e, 'scene lookup failed') };
    }
  }

  async function saveReward(device: GoveeDevice, draft: RewardDraft): Promise<GoveeResult> {
    const cur = await read();
    const existingId = cur.bindings.find((b) => b.device === device.device)?.rewardId ?? '';
    return saveRewardFlow(userId, { draft, prompt: REWARD_PROMPT, existingId }, (reward) =>
      writeBindings(cur.enabled, [
        ...cur.bindings.filter((b) => b.device !== device.device),
        bindingFromReply(device, draft, reward)
      ])
    );
  }

  async function deleteReward(deviceId: string): Promise<GoveeResult> {
    const cur = await read();
    const target = cur.bindings.find((b) => b.device === deviceId);
    if (!target) return { ok: true };
    if (target.rewardId) {
      const remote = await deleteRemoteReward(userId, target.rewardId);
      if (!remote.ok) return remote;
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
    listScenes,
    setEnabled: async (enabled: boolean) => {
      await setModuleEnabled(userId, GOVEE_MODULE, enabled);
      return { ok: true };
    },
    saveReward,
    deleteReward
  };
}
