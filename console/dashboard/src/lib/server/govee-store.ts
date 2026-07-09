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
import type { GoveeOnRedeem, GoveeDevice, GoveeReward, GoveeBinding } from '@bagel/shared';
import { SUB, fabric, invalidate, publishEventSubEnsureOptional } from './services';
import { listModules, upsertModule } from './commands-store';

// Re-export the shared govee shapes so existing importers of this store keep
// working; the definitions live in @bagel/shared for the client components too.
export type { GoveeOnRedeem, GoveeDevice, GoveeReward, GoveeBinding };

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

export interface GoveeView {
  enabled: boolean;
  keyPresent: boolean;
  // bindings is the list of reward->light bindings, one per configured light.
  // The dashboard enforces one reward per light.
  bindings: GoveeBinding[];
}

// RewardDraft is one light's reward + behaviour, bundled so a save is one
// argument. title/cost/color/cooldown are Twitch reward settings; onRedeem/
// replyMessage/allowOff/allowOffline are the binding behaviour sesame reads. The
// light itself (device/sku/name) is passed alongside the draft.
export interface RewardDraft {
  title: string;
  cost: number;
  onRedeem: GoveeOnRedeem;
  // color is the reward tile background ("#rrggbb"); '' leaves Twitch's default.
  color: string;
  // cooldown is the global cooldown in seconds; 0 disables it.
  cooldown: number;
  replyMessage: string;
  allowOff: boolean;
  // allowOffline lifts the live-only gate for this reward (default false).
  allowOffline: boolean;
}

export type GoveeResult = { ok: true } | { ok: false; missingScope?: boolean; error?: string };

function coerceOnRedeem(v: unknown): GoveeOnRedeem {
  return v === 'cancel' || v === 'leave' ? v : 'fulfill';
}

// readBinding coerces a stored "govee" module blob into a normalized binding.
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

// readBindings coerces the stored blob into the list of reward->light bindings,
// tolerating the legacy single-binding blob (top-level rewardId/device) as a
// one-element list so pre-multi-light configs keep rendering.
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

// rewardWire maps a draft to the outgress reward contract. The reward always
// requires input (the colour) and rides the request queue so sesame can resolve
// it.
function rewardWire(draft: RewardDraft, id: string): RewardWire {
  const cooldown = Number.isFinite(draft.cooldown) && draft.cooldown > 0 ? Math.trunc(draft.cooldown) : 0;
  return {
    id: id || undefined,
    title: draft.title,
    cost: draft.cost,
    prompt: REWARD_PROMPT,
    // Empty leaves Twitch's default tile colour rather than sending "".
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

// GoveeStore is the per-broadcaster operation set returned by goveeStore.
export interface GoveeStore {
  read(): Promise<GoveeView>;
  setKey(key: string): Promise<GoveeResult>;
  clearKey(): Promise<GoveeResult>;
  listDevices(): Promise<{ devices: GoveeDevice[]; error?: string }>;
  setEnabled(enabled: boolean): Promise<GoveeResult>;
  // saveReward creates or updates the reward bound to one light.
  saveReward(device: GoveeDevice, draft: RewardDraft): Promise<GoveeResult>;
  // deleteReward removes the reward + binding for one light (by device id).
  deleteReward(deviceId: string): Promise<GoveeResult>;
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
      bindings: row ? readBindings(row.configs) : []
    };
  }

  // writeBindings persists the whole binding list under the module blob's
  // "bindings" key, preserving the module enable flag.
  async function writeBindings(enabled: boolean, bindings: GoveeBinding[]): Promise<void> {
    await upsertModule(userId, GOVEE_MODULE, enabled, { bindings } as unknown as Record<string, unknown>);
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

  async function saveReward(device: GoveeDevice, draft: RewardDraft): Promise<GoveeResult> {
    const cur = await read();
    const existing = cur.bindings.find((b) => b.device === device.device);
    const existingId = existing?.rewardId ?? '';
    const verb = existingId ? 'update' : 'create';
    const req: Record<string, unknown> = { reward: rewardWire(draft, existingId) };
    if (existingId) req.reward_id = existingId;

    const reply = await callReward(userId, verb, req);
    if (reply.missing_scope) return { ok: false, missingScope: true };
    if (reply.error || !reply.reward) return { ok: false, error: reply.error ?? `${verb} failed` };

    const rewardId = reply.reward.id ?? existingId;
    // Mirror the settings Twitch echoed back (falling back to the draft) so the
    // editor re-populates the colour + cooldown exactly as stored.
    const color = reply.reward.background_color ?? draft.color;
    const cooldown = reply.reward.global_cooldown_enabled ? reply.reward.global_cooldown_seconds : 0;
    const binding: GoveeBinding = {
      device: device.device,
      sku: device.sku,
      deviceName: device.name,
      onRedeem: draft.onRedeem,
      rewardId,
      reward: { rewardId, title: reply.reward.title, cost: reply.reward.cost, color, cooldown },
      allowOffline: draft.allowOffline,
      allowOff: draft.allowOff,
      replyMessage: draft.replyMessage
    };
    // One reward per light: replace any existing binding for this device.
    const bindings = [...cur.bindings.filter((b) => b.device !== device.device), binding];
    await writeBindings(cur.enabled, bindings);
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
      const cur = await read();
      await writeBindings(enabled, cur.bindings);
      return { ok: true };
    },
    saveReward,
    deleteReward
  };
}
