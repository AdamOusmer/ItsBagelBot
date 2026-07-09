// Govee store: the setup surface for the "recolour my lights" reward.
//
// Three homes, one page:
//
//   - The Govee API key is a secret. It is stored encrypted at rest by the
//     modules service (Tink AEAD, that service's own keyset) and reached here
//     only through set/clear/status verbs — the value never comes back, the UI
//     shows "key on file" or not.
//   - The device list is fetched live from Govee through the gateway
//     (bagel.rpc.gateway.govee.devices), which authenticates with the stored
//     key, so the browser never sees it either.
//   - The reward is a Twitch custom reward (owned by outgress, created under the
//     broadcaster's token, same RPC the Channel Points tab uses) plus a local
//     binding (device + reward id + success policy) stored in the "govee" module
//     blob and read by sesame's govee module.
//
// The per-broadcaster operations are bound to a broadcaster by `goveeStore(id)`,
// which returns them as methods closing over the id, so no operation repeats it
// as an argument. A redemption is driven by sesame; this store only sets up.
import { rpc } from '@bagel/shared/server/nats';
import { POLICY } from '@bagel/shared/server/cache-keys';
import { SUB, fabric, invalidate, publishEventSubEnsureOptional } from './services';
import { listModules, upsertModule } from './commands-store';

const GOVEE_MODULE = 'govee';

// Cache key for the one slow govee read: the third-party device list. Flushed
// when the broadcaster's key changes (a new key can front a different Govee
// account) and rides the coarse per-user flush via userPrefixes() as a bus-gap
// safety net. The key-presence flag is deliberately NOT cached: it is a cheap
// indexed DB read and its write path is off the invalidation bus, so caching it
// per replica could strand a "no key" view on another pod right after a save.
const devicesCacheKey = (userId: string) => `govee-devices:${userId}`;

// The reward always requires input (the colour), always rides Twitch's request
// queue (so sesame can fulfil/refund it), and is prompted with the accepted
// colour formats.
const REWARD_PROMPT = 'Type a colour: a name like blue, or a hex code like #00ccff';

export type GoveeOnRedeem = 'fulfill' | 'cancel' | 'leave';

// GoveeDevice mirrors gatewayrpc.GoveeDevice (snake/camel already aligned).
export interface GoveeDevice {
  device: string;
  sku: string;
  name: string;
  color: boolean;
}

// GoveeReward is the dashboard mirror of the Twitch reward, kept in the blob so
// the page renders without a Twitch round trip.
export interface GoveeReward {
  rewardId: string;
  title: string;
  cost: number;
}

// GoveeBinding is the module blob shape. device/sku/onRedeem/rewardId/allowOffline
// are the fields sesame reads; reward is the dashboard-only display mirror.
export interface GoveeBinding {
  device: string;
  sku: string;
  deviceName: string;
  onRedeem: GoveeOnRedeem;
  rewardId: string;
  reward: GoveeReward | null;
  // allowOffline opts out of the live-only gate (default false = live only).
  // sesame reads this same flag; the dashboard only sets it true behind a warning.
  allowOffline: boolean;
}

export interface GoveeView {
  enabled: boolean;
  keyPresent: boolean;
  binding: GoveeBinding;
}

// RewardDraft is the reward's editable shape, bundled so a save is one argument.
export interface RewardDraft {
  title: string;
  cost: number;
  onRedeem: GoveeOnRedeem;
}

export type GoveeResult = { ok: true } | { ok: false; missingScope?: boolean; error?: string };

function blankBinding(): GoveeBinding {
  return { device: '', sku: '', deviceName: '', onRedeem: 'fulfill', rewardId: '', reward: null, allowOffline: false };
}

function coerceOnRedeem(v: unknown): GoveeOnRedeem {
  return v === 'cancel' || v === 'leave' ? v : 'fulfill';
}

// readBinding coerces a stored "govee" module blob into a normalized binding.
function readBinding(configs: unknown): GoveeBinding {
  const c = (configs ?? {}) as Partial<GoveeBinding>;
  const reward = c.reward && typeof c.reward === 'object' ? (c.reward as GoveeReward) : null;
  return {
    device: String(c.device ?? ''),
    sku: String(c.sku ?? ''),
    deviceName: String(c.deviceName ?? ''),
    onRedeem: coerceOnRedeem(c.onRedeem),
    rewardId: String(c.rewardId ?? ''),
    reward: reward
      ? { rewardId: String(reward.rewardId ?? ''), title: String(reward.title ?? ''), cost: Number(reward.cost ?? 0) }
      : null,
    allowOffline: c.allowOffline === true
  };
}

interface RewardWire {
  id?: string;
  title: string;
  cost: number;
  prompt?: string;
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

// rewardWire maps a draft to the outgress reward contract. The reward always
// requires input (the colour) and rides the request queue so sesame can resolve
// it.
function rewardWire(draft: RewardDraft, id: string): RewardWire {
  return {
    id: id || undefined,
    title: draft.title,
    cost: draft.cost,
    prompt: REWARD_PROMPT,
    is_enabled: true,
    is_paused: false,
    is_user_input_required: true,
    should_skip_queue: false,
    max_per_stream_enabled: false,
    max_per_stream: 1,
    max_per_user_per_stream_enabled: false,
    max_per_user_per_stream: 1,
    global_cooldown_enabled: false,
    global_cooldown_seconds: 5
  };
}

function callReward(userId: string, verb: string, req: Record<string, unknown>): Promise<RewardReplyWire> {
  return rpc<RewardReplyWire>(`${SUB.outgressRpc}.channelpoints.${verb}`, { broadcaster_id: userId, ...req }, 8000);
}

// GoveeStore is the per-broadcaster operation set returned by goveeStore.
export interface GoveeStore {
  read(): Promise<GoveeView>;
  setKey(key: string): Promise<GoveeResult>;
  clearKey(): Promise<GoveeResult>;
  listDevices(): Promise<{ devices: GoveeDevice[]; error?: string }>;
  setDevice(device: GoveeDevice): Promise<GoveeResult>;
  setEnabled(enabled: boolean): Promise<GoveeResult>;
  setAllowOffline(allowOffline: boolean): Promise<GoveeResult>;
  saveReward(draft: RewardDraft): Promise<GoveeResult>;
  deleteReward(): Promise<GoveeResult>;
}

// goveeStore binds every per-broadcaster operation to one broadcaster id.
export function goveeStore(userId: string): GoveeStore {
  // keyPresent is a direct (uncached) status read: cheap, always authoritative,
  // and safe to run on every load. A blip degrades to false, exactly as before.
  async function keyPresent(): Promise<boolean> {
    try {
      const r = await rpc<{ present?: boolean }>(`${SUB.goveeKey}.status`, { user_id: userId }, 3000);
      return !!r.present;
    } catch {
      return false;
    }
  }

  async function read(): Promise<GoveeView> {
    // The module blob (listModules) and the key-presence flag are independent
    // reads; run them together so the page's server load is one round trip deep,
    // not two.
    const [rows, present] = await Promise.all([listModules(userId), keyPresent()]);
    const row = rows.find((r) => r.name === GOVEE_MODULE);
    return {
      enabled: row ? row.is_enabled : false,
      keyPresent: present,
      binding: row ? readBinding(row.configs) : blankBinding()
    };
  }

  async function writeBinding(enabled: boolean, binding: GoveeBinding): Promise<void> {
    await upsertModule(userId, GOVEE_MODULE, enabled, binding as unknown as Record<string, unknown>);
  }

  // patchBinding merges a partial change into the current binding, preserving the
  // module enable flag.
  async function patchBinding(patch: Partial<GoveeBinding>): Promise<GoveeResult> {
    const cur = await read();
    await writeBinding(cur.enabled, { ...cur.binding, ...patch });
    return { ok: true };
  }

  async function setKey(key: string): Promise<GoveeResult> {
    const r = await rpc<{ error?: string }>(`${SUB.goveeKey}.set`, { user_id: userId, key }, 3000);
    if (r.error) return { ok: false, error: r.error };
    // A new key can front a different Govee account: drop the cached device list
    // so the next read reflects the new account immediately.
    invalidate(devicesCacheKey(userId));
    return { ok: true };
  }

  async function clearKey(): Promise<GoveeResult> {
    const r = await rpc<{ error?: string }>(`${SUB.goveeKey}.clear`, { user_id: userId }, 3000);
    if (r.error) return { ok: false, error: r.error };
    invalidate(devicesCacheKey(userId));
    return { ok: true };
  }

  // listDevices is SWR-cached (POLICY.govee). Only a successful device list is
  // cached; on any error the loader throws so the fabric caches nothing and, if
  // a recent list exists, serves it stale instead of flashing an error. A cold
  // error surfaces as { devices: [], error }, same shape as before.
  async function listDevices(): Promise<{ devices: GoveeDevice[]; error?: string }> {
    try {
      const devices = await fabric.readKey(devicesCacheKey(userId), POLICY.govee, async () => {
        const r = await rpc<{ devices?: GoveeDevice[]; error?: string }>(
          `${SUB.gateway}.govee.devices`,
          { channel_id: userId },
          // Just over the gateway's devices handler budget (8s) so this RPC
          // never abandons a fetch the gateway is still completing.
          9000
        );
        if (r.error) throw new Error(r.error);
        return Array.isArray(r.devices) ? r.devices : [];
      });
      return { devices };
    } catch (e) {
      return { devices: [], error: e instanceof Error ? e.message : 'device lookup failed' };
    }
  }

  async function saveReward(draft: RewardDraft): Promise<GoveeResult> {
    const cur = await read();
    const existingId = cur.binding.rewardId;
    const verb = existingId ? 'update' : 'create';
    const req: Record<string, unknown> = { reward: rewardWire(draft, existingId) };
    if (existingId) req.reward_id = existingId;

    const reply = await callReward(userId, verb, req);
    if (reply.missing_scope) return { ok: false, missingScope: true };
    if (reply.error || !reply.reward) return { ok: false, error: reply.error ?? `${verb} failed` };

    const rewardId = reply.reward.id ?? existingId;
    await writeBinding(cur.enabled, {
      ...cur.binding,
      onRedeem: draft.onRedeem,
      rewardId,
      reward: { rewardId, title: reply.reward.title, cost: reply.reward.cost }
    });
    if (!existingId) await publishEventSubEnsureOptional(userId);
    return { ok: true };
  }

  async function deleteReward(): Promise<GoveeResult> {
    const cur = await read();
    if (!cur.binding.rewardId) return { ok: true };
    const reply = await callReward(userId, 'delete', { reward_id: cur.binding.rewardId });
    if (reply.missing_scope) return { ok: false, missingScope: true };
    if (reply.error) return { ok: false, error: reply.error };
    await writeBinding(cur.enabled, { ...cur.binding, rewardId: '', reward: null });
    return { ok: true };
  }

  return {
    read,
    setKey,
    clearKey,
    listDevices,
    setDevice: (device: GoveeDevice) => patchBinding({ device: device.device, sku: device.sku, deviceName: device.name }),
    setEnabled: async (enabled: boolean) => {
      const cur = await read();
      await writeBinding(enabled, cur.binding);
      return { ok: true };
    },
    setAllowOffline: (allowOffline: boolean) => patchBinding({ allowOffline }),
    saveReward,
    deleteReward
  };
}
