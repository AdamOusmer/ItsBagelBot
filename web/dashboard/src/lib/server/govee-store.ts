// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc, rpcReply } from '@bagel/kit/server/nats';
import { POLICY } from '@bagel/kit/server/cache-keys';
import { type GoveeOnRedeem, type GoveeDevice, type GoveeReward, type GoveeBinding, MOD } from '@bagel/kit';
import { SUB, fabric, invalidate } from './services';
import { upsertModule } from './commands-store';
import { readModuleBlob, setModuleEnabled } from './module-blob';
import { deleteRemoteReward, saveRewardFlow, type MirroredReward } from './reward-wire';
import type { RewardCore } from './reward-form';

export type { GoveeOnRedeem, GoveeDevice, GoveeReward, GoveeBinding };

const GOVEE_MODULE = MOD.govee;

const devicesCacheKey = (userId: string) => `govee-devices:${userId}`;

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

export interface GoveeStore {
  read(): Promise<GoveeView>;
  setKey(key: string): Promise<GoveeResult>;
  clearKey(): Promise<GoveeResult>;
  listDevices(): Promise<{ devices: GoveeDevice[]; error?: string; code?: 'key_rejected' | 'unreachable' }>;
  setEnabled(enabled: boolean): Promise<GoveeResult>;
  saveReward(device: GoveeDevice, draft: RewardDraft): Promise<GoveeResult>;
  deleteReward(deviceId: string): Promise<GoveeResult>;
}

function deviceLookupFailure(e: unknown): { devices: GoveeDevice[]; error: string; code: 'key_rejected' | 'unreachable' } {
  return { devices: [], error: e instanceof Error ? e.message : 'device lookup failed', code: (e as { keyRejected?: boolean } | null)?.keyRejected === true ? 'key_rejected' : 'unreachable' };
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
    invalidate(devicesCacheKey(userId));
    return { ok: true };
  }

  async function listDevices(): Promise<{ devices: GoveeDevice[]; error?: string; code?: 'key_rejected' | 'unreachable' }> {
    try {
      const devices = await fabric.readKey(devicesCacheKey(userId), POLICY.govee, async () => {
        const r = await rpcReply<{ devices?: GoveeDevice[]; key_rejected?: boolean; error?: string }>(
          `${SUB.gossip}.govee.devices`,
          { channel_id: userId },
          // Just over gossip's 8 s devices handler budget.
          9000
        );
        if (r.key_rejected === true) throw Object.assign(new Error(r.error ?? 'key rejected'), { keyRejected: true });
        else if (r.error) throw new Error(r.error);
        else return Array.isArray(r.devices) ? r.devices : [];
      });
      return { devices };
    } catch (e) {
      return deviceLookupFailure(e);
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
    setEnabled: async (enabled: boolean) => {
      await setModuleEnabled(userId, GOVEE_MODULE, enabled);
      return { ok: true };
    },
    saveReward,
    deleteReward
  };
}
