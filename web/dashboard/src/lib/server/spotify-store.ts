// Copyright (c) 2026 Adam Ousmer. All rights reserved.

import { rpc, rpcReply } from '@bagel/kit/server/nats';
import type {
  SpotifySrConfig,
  SpotifyQuotas,
  SpotifyRedeemConfig,
  SpotifyReward,
  RewardOnRedeem,
  SpotifySrPerm
} from '@bagel/kit';
import { blankSpotifyRedeem, blankSpotifySr, blankSpotifyQuotas } from '@bagel/kit';
import { SUB, publishEventSubEnsureOptional } from './services';
import { upsertModule } from './commands-store';
import { readModuleBlob } from './module-blob';

export type {
  SpotifySrConfig,
  SpotifyRedeemConfig,
  SpotifyReward,
  RewardOnRedeem,
  SpotifySrPerm,
  SpotifyQuotas
};

const SONGQUEUE_MODULE = 'songqueue';

const REWARD_PROMPT = 'Type a song: a name like Never Gonna Give You Up, "artist - song", or paste a Spotify link';

export interface SpotifyView {
  enabled: boolean;
  sr: SpotifySrConfig;
  redeem: SpotifyRedeemConfig;
  quotas: SpotifyQuotas;
}

export interface SpotifyGrant {
  connected: boolean;
  scopes: string[];
}

export interface RewardDraft {
  title: string;
  cost: number;
  onRedeem: RewardOnRedeem;
  color: string;
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

function readSr(raw: Partial<SpotifySrConfig> | undefined): SpotifySrConfig {
  const sr = blankSpotifySr();
  if (!raw || typeof raw !== 'object') return { ...sr, enabled: true };
  sr.enabled = raw.enabled !== false;
  sr.perm = coercePerm(raw.perm);
  sr.allowOffline = raw.allowOffline === true;
  return sr;
}

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
  redeem.allowOffline = raw.allowOffline === true;
  redeem.reward = readReward(raw.reward, redeem.rewardId);
  return redeem;
}

function readQuotas(raw: Partial<SpotifyQuotas> | undefined): SpotifyQuotas {
  const q = blankSpotifyQuotas();
  if (!raw || typeof raw !== 'object') return q;
  for (const tier of ['everyone', 'sub', 'vip', 'mod'] as const) {
    const v = raw[tier];
    q[tier] = typeof v === 'number' && Number.isFinite(v) && v > 0 ? Math.floor(v) : null;
  }
  return q;
}

function readView(configs: unknown): SpotifyView {
  const c = (configs ?? {}) as {
    sr?: Partial<SpotifySrConfig>;
    redeem?: Partial<SpotifyRedeemConfig> & { reward?: Partial<SpotifyReward> | null };
    quotas?: Partial<SpotifyQuotas>;
  };
  return { enabled: false, sr: readSr(c.sr), redeem: readRedeem(c.redeem), quotas: readQuotas(c.quotas) };
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

export interface SpotifyApp {
  present: boolean;
  clientId: string;
}

export interface SpotifyStore {
  read(): Promise<SpotifyView>;
  grant(): Promise<SpotifyGrant>;
  app(): Promise<SpotifyApp>;
  saveApp(clientId: string, clientSecret: string): Promise<SpotifyResult>;
  clearApp(): Promise<SpotifyResult>;
  setEnabled(enabled: boolean): Promise<SpotifyResult>;
  saveSr(sr: SpotifySrConfig): Promise<SpotifyResult>;
  saveQuotas(quotas: SpotifyQuotas): Promise<SpotifyResult>;
  setRedeemPath(path: { enabled: boolean; allowOffline: boolean }): Promise<SpotifyResult>;
  saveReward(draft: RewardDraft): Promise<SpotifyResult>;
  deleteReward(): Promise<SpotifyResult>;
  disconnect(): Promise<SpotifyResult>;
}

export function spotifyStore(userId: string): SpotifyStore {
  async function rawConfigs(): Promise<Record<string, unknown>> {
    const { configs } = await readModuleBlob<Record<string, unknown>>(userId, SONGQUEUE_MODULE);
    return { ...configs };
  }

  async function read(): Promise<SpotifyView> {
    const { enabled, configs } = await readModuleBlob<unknown>(userId, SONGQUEUE_MODULE);
    const view = readView(configs);
    view.enabled = enabled;
    return view;
  }

  async function grant(): Promise<SpotifyGrant> {
    try {
      const r = await rpc<{ present?: boolean; scopes?: string[] }>(
        `${SUB.spotifyKey}.status`,
        { user_id: userId },
        3000
      );
      return { connected: !!r.present, scopes: Array.isArray(r.scopes) ? r.scopes : [] };
    } catch {
      return { connected: false, scopes: [] };
    }
  }

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
    const r = await rpcReply<{ error?: string }>(
      `${SUB.spotifyKey}.app.set`,
      { user_id: userId, client_id: clientId, client_secret: clientSecret },
      5000
    );
    if (r.error) return { ok: false, error: r.error };
    return { ok: true };
  }

  async function clearApp(): Promise<SpotifyResult> {
    const r = await rpcReply<{ error?: string }>(`${SUB.spotifyKey}.app.clear`, { user_id: userId }, 5000);
    if (r.error) return { ok: false, error: r.error };
    return { ok: true };
  }

  async function writeBlob(
    enabled: boolean,
    sr: SpotifySrConfig,
    redeem: SpotifyRedeemConfig,
    quotas: SpotifyQuotas
  ): Promise<void> {
    const base = await rawConfigs();
    await upsertModule(userId, SONGQUEUE_MODULE, enabled, {
      ...base,
      quotas,
      sr: { enabled: sr.enabled, perm: sr.perm, allowOffline: sr.allowOffline },
      redeem: {
        enabled: redeem.enabled,
        rewardId: redeem.rewardId,
        onRedeem: redeem.onRedeem,
        replyMessage: redeem.replyMessage,
        allowOffline: redeem.allowOffline,
        ...(redeem.reward ? { reward: redeem.reward } : {})
      }
    } as unknown as Record<string, unknown>);
  }

  async function setEnabled(enabled: boolean): Promise<SpotifyResult> {
    const cur = await read();
    await writeBlob(enabled, cur.sr, cur.redeem, cur.quotas);
    return { ok: true };
  }

  async function saveQuotas(quotas: SpotifyQuotas): Promise<SpotifyResult> {
    const cur = await read();
    await writeBlob(cur.enabled, cur.sr, cur.redeem, quotas);
    return { ok: true };
  }

  async function saveSr(sr: SpotifySrConfig): Promise<SpotifyResult> {
    const cur = await read();
    await writeBlob(cur.enabled, sr, cur.redeem, cur.quotas);
    return { ok: true };
  }

  async function setRedeemPath(path: { enabled: boolean; allowOffline: boolean }): Promise<SpotifyResult> {
    const cur = await read();
    await writeBlob(cur.enabled, cur.sr, { ...cur.redeem, ...path }, cur.quotas);
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
    await writeBlob(cur.enabled, cur.sr, redeem, cur.quotas);
    if (!existingId) await publishEventSubEnsureOptional(userId);
    return { ok: true };
  }

  async function deleteReward(): Promise<SpotifyResult> {
    const cur = await read();
    if (!cur.redeem.rewardId) return { ok: true };
    const reply = await callReward(userId, 'delete', { reward_id: cur.redeem.rewardId });
    if (reply.missing_scope) return { ok: false, missingScope: true };
    if (reply.error) return { ok: false, error: reply.error };
    await writeBlob(cur.enabled, cur.sr, { ...blankSpotifyRedeem() }, cur.quotas);
    return { ok: true };
  }

  async function disconnect(): Promise<SpotifyResult> {
    const r = await rpcReply<{ error?: string }>(`${SUB.spotifyKey}.clear`, { user_id: userId }, 3000);
    if (r.error) return { ok: false, error: r.error };
    return { ok: true };
  }

  return {
    read,
    grant,
    app,
    saveApp,
    clearApp,
    setEnabled,
    saveSr,
    saveQuotas,
    setRedeemPath,
    saveReward,
    deleteReward,
    disconnect
  };
}
