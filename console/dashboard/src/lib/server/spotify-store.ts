// Copyright (c) 2026 Adam Ousmer. All rights reserved.

// Spotify store: the setup surface for song requests.
//
// Four homes, one page:
//
//   - The broadcaster's OWN Spotify application (client id + secret) is pasted
//     here and sealed by the modules service. There is no fleet-wide app any
//     more, so this is the FIRST setup step: without it there is nothing to
//     authorize against. The secret is write-only from the console's side —
//     status echoes the (public) client id and nothing else.
//   - The Spotify account is connected through OAuth against that application
//     (routes/spotify/connect + callback): the browser never sees a token, the
//     code is redeemed by gossip (the only service holding the client secret),
//     and the refresh token is sealed by the modules service
//     (bagel.rpc.modules.spotify.*) exactly like the Govee key. This store only
//     reads/writes presence.
//   - The two request paths — chat (!sr) and channel points — are switches and
//     settings inside the "songqueue" module blob (the ModuleView key sesame's
//     songqueue module registers). They are independent halves: either can be
//     on while the other is off, or both at once. The blob shape:
//
//       { // songqueueConfig fields — already read by app/sesame/modules/songqueue.go:
//         "maxDepth": number,
//         "addMessage" | "playingMessage" | "retractMessage": string,
//         // request-path switches — written here, consumed by the wiring that
//         // lands with the redemption path:
//         "sr": { "enabled": bool, "perm": "everyone"|"sub"|"vip"|"mod"|"broadcaster" },
//         "redeem": { "enabled": bool, "rewardId": string, "onRedeem":
//                     "fulfill"|"cancel"|"leave", "replyMessage": string,
//                     "reward": { rewardId, title, cost, color, cooldown } | null } }
//
//     Writes spread whatever is stored first and overlay sr/redeem on top, so
//     the songqueueConfig keys this page does not manage survive a save. The
//     perm strings are exactly what the engine's ParsePerm accepts, so the
//     command gate can read them straight from the blob.
//   - The channel-points reward itself is a Twitch entity owned by outgress
//     (same RPC the Channel Points tab uses), created under the broadcaster's
//     token with user input REQUIRED — the typed input is the song query.
//   - The enable row rides the standard modules service (listModules /
//     upsertModule), so the tile toggle, the projection and cache invalidation
//     all behave like every other named module.
import { rpc } from '@bagel/shared/server/nats';
import type {
  SpotifySrConfig,
  SpotifyRedeemConfig,
  SpotifyReward,
  RewardOnRedeem,
  SpotifySrPerm
} from '@bagel/shared';
import { blankSpotifyRedeem, blankSpotifySr } from '@bagel/shared';
import { SUB, publishEventSubEnsureOptional } from './services';
import { listModules, upsertModule } from './commands-store';

export type { SpotifySrConfig, SpotifyRedeemConfig, SpotifyReward, RewardOnRedeem, SpotifySrPerm };

const SONGQUEUE_MODULE = 'songqueue';

// The reward always requires input (the song query), always rides Twitch's
// request queue (so sesame can fulfil/refund it), and prompts with what to type.
const REWARD_PROMPT = 'Type a song: a name like Never Gonna Give You Up, "artist - song", or paste a Spotify link';

export interface SpotifyView {
  enabled: boolean;
  sr: SpotifySrConfig;
  redeem: SpotifyRedeemConfig;
}

// RewardDraft is one save's worth of reward + behaviour: title/cost/color/
// cooldown are Twitch reward settings; onRedeem/replyMessage are the binding
// behaviour sesame reads.
export interface RewardDraft {
  title: string;
  cost: number;
  onRedeem: RewardOnRedeem;
  // color is the reward tile background ("#rrggbb"); '' leaves Twitch's default.
  color: string;
  // cooldown is the global cooldown in seconds; 0 disables it.
  cooldown: number;
  replyMessage: string;
}

export type SpotifyResult = { ok: true } | { ok: false; missingScope?: boolean; error?: string };

function coercePerm(v: unknown): SpotifySrPerm {
  return v === 'sub' || v === 'vip' || v === 'mod' || v === 'broadcaster' ? v : 'everyone';
}

function coerceOnRedeem(v: unknown): RewardOnRedeem {
  return v === 'cancel' || v === 'leave' ? v : 'fulfill';
}

// readView coerces the stored "spotify" blob into both request-path configs,
// filling blanks for anything missing so a partial or legacy blob renders.
// The three readers below mirror coercePerm/coerceOnRedeem above: one function
// per thing being coerced out of the opaque config blob. readView used to
// inline all of it, which meant the song-request half and the channel-point
// half — which share no fields and no rules — had to be read as one unit.

function readSr(raw: Partial<SpotifySrConfig> | undefined): SpotifySrConfig {
  const sr = blankSpotifySr();
  if (!raw || typeof raw !== 'object') return sr;
  sr.enabled = raw.enabled === true;
  sr.perm = coercePerm(raw.perm);
  return sr;
}

// readReward returns the mirrored snapshot of the Twitch reward, or null.
//
// A reward id without its snapshot still means bound: the caller keeps the id
// so edit/delete target the right Twitch reward even if the mirror was lost.
// Sequential guards rather than one compound test: each line rejects for its
// own reason, so the shape of a usable snapshot is readable top to bottom.
function mirroredReward(raw: Partial<SpotifyReward> | null | undefined): SpotifyReward | null {
  if (!raw) return null;
  if (typeof raw !== 'object') return null;
  if (!raw.rewardId) return null;
  return {
    rewardId: String(raw.rewardId),
    title: String(raw.title ?? ''),
    cost: Number(raw.cost ?? 0),
    color: String(raw.color ?? ''),
    cooldown: Number(raw.cooldown ?? 0)
  };
}

function readReward(raw: Partial<SpotifyReward> | null | undefined, rewardId: string): SpotifyReward | null {
  const mirrored = mirroredReward(raw);
  if (mirrored) return mirrored;
  if (rewardId) return { rewardId, title: '', cost: 0, color: '', cooldown: 0 };
  return null;
}

function readRedeem(
  raw: (Partial<SpotifyRedeemConfig> & { reward?: Partial<SpotifyReward> | null }) | undefined
): SpotifyRedeemConfig {
  const redeem = blankSpotifyRedeem();
  if (!raw || typeof raw !== 'object') return redeem;
  redeem.enabled = raw.enabled === true;
  redeem.rewardId = String(raw.rewardId ?? '');
  redeem.onRedeem = coerceOnRedeem(raw.onRedeem);
  redeem.replyMessage = String(raw.replyMessage ?? '');
  redeem.reward = readReward(raw.reward, redeem.rewardId);
  return redeem;
}

function readView(configs: unknown): SpotifyView {
  const c = (configs ?? {}) as {
    sr?: Partial<SpotifySrConfig>;
    redeem?: Partial<SpotifyRedeemConfig> & { reward?: Partial<SpotifyReward> | null };
  };
  return { enabled: false, sr: readSr(c.sr), redeem: readRedeem(c.redeem) };
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

// rewardWire maps a draft onto the outgress reward contract. The reward always
// requires input (the song query) and rides the request queue so sesame can
// resolve it.
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

// SpotifyStore is the per-broadcaster operation set returned by spotifyStore.
// SpotifyApp is the presence of a broadcaster's OWN registered Spotify
// application. The client id is public by construction (it rides the authorize
// URL through the browser) so it is echoed back for display; the secret never
// leaves the modules service's sealed custody, so there is nothing else to
// show.
export interface SpotifyApp {
  present: boolean;
  clientId: string;
}

export interface SpotifyStore {
  read(): Promise<SpotifyView>;
  connected(): Promise<boolean>;
  app(): Promise<SpotifyApp>;
  saveApp(clientId: string, clientSecret: string): Promise<SpotifyResult>;
  clearApp(): Promise<SpotifyResult>;
  setEnabled(enabled: boolean): Promise<SpotifyResult>;
  saveSr(sr: SpotifySrConfig): Promise<SpotifyResult>;
  setRedeemEnabled(enabled: boolean): Promise<SpotifyResult>;
  saveReward(draft: RewardDraft): Promise<SpotifyResult>;
  deleteReward(): Promise<SpotifyResult>;
  disconnect(): Promise<SpotifyResult>;
}

// spotifyStore binds every per-broadcaster operation to one broadcaster id.
export function spotifyStore(userId: string): SpotifyStore {
  // rawRow returns the module row's raw configs blob (or an empty object), so
  // writes can spread-and-overlay instead of clobbering keys this page does
  // not manage (the songqueueConfig message templates, maxDepth).
  async function rawConfigs(): Promise<Record<string, unknown>> {
    const rows = await listModules(userId);
    const row = rows.find((r) => r.name === SONGQUEUE_MODULE);
    return row ? { ...((row.configs ?? {}) as Record<string, unknown>) } : {};
  }

  async function read(): Promise<SpotifyView> {
    const rows = await listModules(userId);
    const row = rows.find((r) => r.name === SONGQUEUE_MODULE);
    const view = readView(row?.configs);
    view.enabled = row ? row.is_enabled : false;
    return view;
  }

  // connected is a direct (uncached) presence read: cheap, always
  // authoritative, and safe to run on every load. A blip degrades to false,
  // exactly like the Govee key-presence flag.
  async function connected(): Promise<boolean> {
    try {
      const r = await rpc<{ present?: boolean }>(`${SUB.spotifyKey}.status`, { user_id: userId }, 3000);
      return !!r.present;
    } catch {
      return false;
    }
  }

  // app is a presence read like connected(): cheap, authoritative, and safe on
  // every load. A blip degrades to "no app", which shows the setup card rather
  // than a connect button that could only fail.
  async function app(): Promise<SpotifyApp> {
    try {
      const r = await rpc<{ present?: boolean; client_id?: string }>(
        `${SUB.spotifyKey}.app.status`,
        { user_id: userId },
        3000
      );
      return { present: !!r.present, clientId: r.client_id ?? '' };
    } catch {
      return { present: false, clientId: '' };
    }
  }

  async function saveApp(clientId: string, clientSecret: string): Promise<SpotifyResult> {
    const r = await rpc<{ error?: string }>(
      `${SUB.spotifyKey}.app.set`,
      { user_id: userId, client_id: clientId, client_secret: clientSecret },
      5000
    );
    if (r.error) return { ok: false, error: r.error };
    return { ok: true };
  }

  // clearApp drops the grant with the application: a refresh token outlives
  // nothing useful once the app that minted it is gone (see the modules-side
  // ClearApp), and leaving one behind would only fail confusingly later.
  async function clearApp(): Promise<SpotifyResult> {
    const r = await rpc<{ error?: string }>(`${SUB.spotifyKey}.app.clear`, { user_id: userId }, 5000);
    if (r.error) return { ok: false, error: r.error };
    return { ok: true };
  }

  // writeBlob persists both halves under one blob, spreading whatever is
  // stored first (upsert replaces the whole configs value, so the overlay
  // keeps the songqueueConfig keys alive).
  async function writeBlob(enabled: boolean, sr: SpotifySrConfig, redeem: SpotifyRedeemConfig): Promise<void> {
    const base = await rawConfigs();
    await upsertModule(userId, SONGQUEUE_MODULE, enabled, {
      ...base,
      sr: { enabled: sr.enabled, perm: sr.perm },
      redeem: {
        enabled: redeem.enabled,
        rewardId: redeem.rewardId,
        onRedeem: redeem.onRedeem,
        replyMessage: redeem.replyMessage,
        ...(redeem.reward ? { reward: redeem.reward } : {})
      }
    } as unknown as Record<string, unknown>);
  }

  async function setEnabled(enabled: boolean): Promise<SpotifyResult> {
    const cur = await read();
    await writeBlob(enabled, cur.sr, cur.redeem);
    return { ok: true };
  }

  async function saveSr(sr: SpotifySrConfig): Promise<SpotifyResult> {
    const cur = await read();
    await writeBlob(cur.enabled, sr, cur.redeem);
    return { ok: true };
  }

  async function setRedeemEnabled(enabled: boolean): Promise<SpotifyResult> {
    const cur = await read();
    await writeBlob(cur.enabled, cur.sr, { ...cur.redeem, enabled });
    return { ok: true };
  }

  async function saveReward(draft: RewardDraft): Promise<SpotifyResult> {
    const cur = await read();
    const existingId = cur.redeem.rewardId;
    const verb = existingId ? 'update' : 'create';
    const req: Record<string, unknown> = { reward: rewardWire(draft, existingId) };
    if (existingId) req.reward_id = existingId;

    const reply = await callReward(userId, verb, req);
    if (reply.missing_scope) return { ok: false, missingScope: true };
    if (reply.error || !reply.reward) return { ok: false, error: reply.error ?? `${verb} failed` };

    // Mirror what Twitch echoed so the editor re-populates, then bind it.
    const rewardId = reply.reward.id ?? existingId;
    const redeem: SpotifyRedeemConfig = {
      ...cur.redeem,
      enabled: cur.redeem.enabled,
      rewardId,
      reward: {
        rewardId,
        title: reply.reward.title,
        cost: reply.reward.cost,
        color: reply.reward.background_color ?? draft.color,
        cooldown: reply.reward.global_cooldown_enabled ? reply.reward.global_cooldown_seconds : 0
      }
    };
    await writeBlob(cur.enabled, cur.sr, redeem);
    if (!existingId) await publishEventSubEnsureOptional(userId);
    return { ok: true };
  }

  async function deleteReward(): Promise<SpotifyResult> {
    const cur = await read();
    if (!cur.redeem.rewardId) return { ok: true };
    const reply = await callReward(userId, 'delete', { reward_id: cur.redeem.rewardId });
    if (reply.missing_scope) return { ok: false, missingScope: true };
    if (reply.error) return { ok: false, error: reply.error };
    await writeBlob(cur.enabled, cur.sr, { ...blankSpotifyRedeem() });
    return { ok: true };
  }

  async function disconnect(): Promise<SpotifyResult> {
    const r = await rpc<{ error?: string }>(`${SUB.spotifyKey}.clear`, { user_id: userId }, 3000);
    if (r.error) return { ok: false, error: r.error };
    return { ok: true };
  }

  return {
    read,
    connected,
    app,
    saveApp,
    clearApp,
    setEnabled,
    saveSr,
    setRedeemEnabled,
    saveReward,
    deleteReward,
    disconnect
  };
}
