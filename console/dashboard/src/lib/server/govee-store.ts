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
// A redemption is driven by sesame: it live-gates, parses the colour and calls
// the gateway. This store only sets things up.
import { rpc } from '@bagel/shared/server/nats';
import { SUB, publishEventSubEnsureOptional } from './services';
import { listModules, upsertModule } from './commands-store';

const GOVEE_MODULE = 'govee';

// The reward always requires input (the colour), always rides Twitch's request
// queue (so sesame can fulfil/refund it), and is prompted with the accepted
// colour formats.
const REWARD_PROMPT = 'Type a colour — a name like blue, or a hex code like #00ccff';

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

// GoveeBinding is the module blob shape. device/sku/onRedeem/rewardId are the
// fields sesame reads; reward is the dashboard-only display mirror.
export interface GoveeBinding {
  device: string;
  sku: string;
  deviceName: string;
  onRedeem: GoveeOnRedeem;
  rewardId: string;
  reward: GoveeReward | null;
}

export interface GoveeView {
  enabled: boolean;
  keyPresent: boolean;
  binding: GoveeBinding;
}

export type GoveeResult = { ok: true } | { ok: false; missingScope?: boolean; error?: string };

function blankBinding(): GoveeBinding {
  return { device: '', sku: '', deviceName: '', onRedeem: 'fulfill', rewardId: '', reward: null };
}

function coerceOnRedeem(v: unknown): GoveeOnRedeem {
  return v === 'cancel' || v === 'leave' ? v : 'fulfill';
}

// readBinding pulls the "govee" module row and normalizes its blob.
function readBinding(configs: unknown): GoveeBinding {
  const c = (configs ?? {}) as Partial<GoveeBinding>;
  const reward = c.reward && typeof c.reward === 'object' ? (c.reward as GoveeReward) : null;
  return {
    device: String(c.device ?? ''),
    sku: String(c.sku ?? ''),
    deviceName: String(c.deviceName ?? ''),
    onRedeem: coerceOnRedeem(c.onRedeem),
    rewardId: String(c.rewardId ?? ''),
    reward: reward ? { rewardId: String(reward.rewardId ?? ''), title: String(reward.title ?? ''), cost: Number(reward.cost ?? 0) } : null
  };
}

// readGovee loads the whole setup view: module enable flag, whether a key is on
// file, and the current binding.
export async function readGovee(userId: string): Promise<GoveeView> {
  const rows = await listModules(userId);
  const row = rows.find((r) => r.name === GOVEE_MODULE);
  const keyPresent = await goveeKeyPresent(userId);
  return {
    enabled: row ? row.is_enabled : false,
    keyPresent,
    binding: row ? readBinding(row.configs) : blankBinding()
  };
}

async function writeBinding(userId: string, enabled: boolean, binding: GoveeBinding): Promise<void> {
  await upsertModule(userId, GOVEE_MODULE, enabled, binding as unknown as Record<string, unknown>);
}

// --- API key custody (modules service, sealed at rest) ---------------------

async function goveeKeyPresent(userId: string): Promise<boolean> {
  try {
    const r = await rpc<{ present?: boolean; error?: string }>(`${SUB.goveeKey}.status`, { user_id: userId }, 3000);
    return !!r.present;
  } catch {
    return false;
  }
}

export async function setGoveeKey(userId: string, key: string): Promise<GoveeResult> {
  const r = await rpc<{ error?: string }>(`${SUB.goveeKey}.set`, { user_id: userId, key }, 3000);
  if (r.error) return { ok: false, error: r.error };
  return { ok: true };
}

export async function clearGoveeKey(userId: string): Promise<GoveeResult> {
  const r = await rpc<{ error?: string }>(`${SUB.goveeKey}.clear`, { user_id: userId }, 3000);
  if (r.error) return { ok: false, error: r.error };
  return { ok: true };
}

// --- device list (via the gateway, using the stored key) -------------------

export async function listGoveeDevices(userId: string): Promise<{ devices: GoveeDevice[]; error?: string }> {
  const r = await rpc<{ devices?: GoveeDevice[]; error?: string }>(
    `${SUB.gateway}.govee.devices`,
    { channel_id: userId },
    8000
  );
  if (r.error) return { devices: [], error: r.error };
  return { devices: Array.isArray(r.devices) ? r.devices : [] };
}

// --- binding writes --------------------------------------------------------

// setDevice records which light the reward drives, preserving the rest of the
// binding.
export async function setGoveeDevice(userId: string, device: string, sku: string, deviceName: string): Promise<GoveeResult> {
  const cur = await readGovee(userId);
  await writeBinding(userId, cur.enabled, { ...cur.binding, device, sku, deviceName });
  return { ok: true };
}

// setEnabled flips whether sesame acts on redemptions at all.
export async function setGoveeEnabled(userId: string, enabled: boolean): Promise<GoveeResult> {
  const cur = await readGovee(userId);
  await writeBinding(userId, enabled, cur.binding);
  return { ok: true };
}

// --- reward (Twitch side + blob mirror) ------------------------------------

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

function rewardWire(title: string, cost: number, id?: string): RewardWire {
  return {
    id: id || undefined,
    title,
    cost,
    prompt: REWARD_PROMPT,
    is_enabled: true,
    is_paused: false,
    // The viewer types the colour, so input is mandatory; and the reward must
    // ride the request queue so sesame can fulfil or refund it.
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

async function callReward(verb: string, req: Record<string, unknown>): Promise<RewardReplyWire> {
  return rpc<RewardReplyWire>(`${SUB.outgressRpc}.channelpoints.${verb}`, req, 8000);
}

// saveGoveeReward creates or updates the one Govee reward on Twitch, then stores
// its id + display mirror + the success policy in the binding. It mirrors the
// Channel Points store: Twitch first (must succeed), then the blob, so the two
// never diverge; a create also (re)ensures the redemption EventSub sub.
export async function saveGoveeReward(
  userId: string,
  title: string,
  cost: number,
  onRedeem: GoveeOnRedeem
): Promise<GoveeResult> {
  const cur = await readGovee(userId);
  const existingId = cur.binding.rewardId;

  const verb = existingId ? 'update' : 'create';
  const req: Record<string, unknown> = { broadcaster_id: userId, reward: rewardWire(title, cost, existingId) };
  if (existingId) req.reward_id = existingId;

  const reply = await callReward(verb, req);
  if (reply.missing_scope) return { ok: false, missingScope: true };
  if (reply.error || !reply.reward) return { ok: false, error: reply.error ?? `${verb} failed` };

  const rewardId = reply.reward.id ?? existingId;
  await writeBinding(userId, cur.enabled, {
    ...cur.binding,
    onRedeem,
    rewardId,
    reward: { rewardId, title: reply.reward.title, cost: reply.reward.cost }
  });
  if (!existingId) await publishEventSubEnsureOptional(userId);
  return { ok: true };
}

// deleteGoveeReward removes the Twitch reward and clears it from the binding.
export async function deleteGoveeReward(userId: string): Promise<GoveeResult> {
  const cur = await readGovee(userId);
  if (!cur.binding.rewardId) return { ok: true };

  const reply = await callReward('delete', { broadcaster_id: userId, reward_id: cur.binding.rewardId });
  if (reply.missing_scope) return { ok: false, missingScope: true };
  if (reply.error) return { ok: false, error: reply.error };

  await writeBinding(userId, cur.enabled, { ...cur.binding, rewardId: '', reward: null });
  return { ok: true };
}
