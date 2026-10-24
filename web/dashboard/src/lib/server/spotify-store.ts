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
import { SUB } from './services';
import { upsertModule } from './commands-store';
import { readModuleBlob } from './module-blob';
import { deleteRemoteReward, saveRewardFlow } from './reward-wire';
import type { RewardCore } from './reward-form';

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
  needsReconnect: boolean;
}

export interface SpotifyPlayerQueue {
  current?: { id: string } | null;
  up_next?: { id: string }[];
  progress_ms?: number;
  duration_ms?: number;
  playing?: boolean;
  error?: string;
}

const PLAYBACK_SCOPE = 'user-modify-playback-state';

export async function readSpotifyPlayerQueue(userId: string): Promise<SpotifyPlayerQueue | null> {
  try {
    const reply = await rpcReply<SpotifyPlayerQueue>(
      `${SUB.gossip}.spotify.playerqueue`,
      { channel_id: userId, progress: true },
      8000
    );
    return reply.error ? null : reply;
  } catch {
    return null;
  }
}

export type RewardDraft = RewardCore;

export type SpotifyResult = { ok: true } | { ok: false; missingScope?: boolean; missingSpotifyScope?: boolean; error?: string; code?: string };

function coercePerm(v: unknown): SpotifySrPerm {
  return v === 'sub' || v === 'vip' || v === 'mod' || v === 'broadcaster' ? v : 'everyone';
}

function coerceOnRedeem(v: unknown): RewardOnRedeem {
  return v === 'cancel' || v === 'leave' ? v : 'fulfill';
}

const isObject = (v: unknown): v is object => !!v && typeof v === 'object';

function readSr(raw: Partial<SpotifySrConfig> | undefined): SpotifySrConfig {
  const sr = blankSpotifySr();
  if (!isObject(raw)) return { ...sr, enabled: true };
  sr.enabled = raw.enabled !== false;
  sr.perm = coercePerm(raw.perm);
  sr.allowOffline = raw.allowOffline === true;
  return sr;
}

function mirroredReward(raw: Partial<SpotifyReward> | null | undefined): SpotifyReward | null {
  if (!isObject(raw)) return null;
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
  if (!isObject(raw)) return redeem;
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
  if (!isObject(raw)) return q;
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
  skip(): Promise<SpotifyResult>;
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
      const r = await rpc<{ present?: boolean; scopes?: string[]; needs_reconnect?: boolean }>(
        `${SUB.spotifyKey}.status`,
        { user_id: userId },
        3000
      );
      return {
        connected: !!r.present,
        scopes: Array.isArray(r.scopes) ? r.scopes : [],
        needsReconnect: r.needs_reconnect === true
      };
    } catch {
      return { connected: false, scopes: [], needsReconnect: false };
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

  async function write(view: SpotifyView): Promise<void> {
    const { enabled, sr, redeem, quotas } = view;
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

  async function patch(change: (cur: SpotifyView) => Partial<SpotifyView>): Promise<SpotifyResult> {
    const cur = await read();
    await write({ ...cur, ...change(cur) });
    return { ok: true };
  }

  const setEnabled = (enabled: boolean) => patch(() => ({ enabled }));
  const saveQuotas = (quotas: SpotifyQuotas) => patch(() => ({ quotas }));
  const saveSr = (sr: SpotifySrConfig) => patch(() => ({ sr }));
  const setRedeemPath = (path: { enabled: boolean; allowOffline: boolean }) =>
    patch((cur) => ({ redeem: { ...cur.redeem, ...path } }));

  async function saveReward(draft: RewardDraft): Promise<SpotifyResult> {
    const cur = await read();
    return saveRewardFlow(userId, { draft, prompt: REWARD_PROMPT, existingId: cur.redeem.rewardId }, (reward) =>
      write({ ...cur, redeem: { ...cur.redeem, rewardId: reward.rewardId, reward } })
    );
  }

  async function deleteReward(): Promise<SpotifyResult> {
    const cur = await read();
    if (!cur.redeem.rewardId) return { ok: true };
    const remote = await deleteRemoteReward(userId, cur.redeem.rewardId);
    if (!remote.ok) return remote;
    await write({ ...cur, redeem: blankSpotifyRedeem() });
    return { ok: true };
  }

  async function disconnect(): Promise<SpotifyResult> {
    const r = await rpcReply<{ error?: string }>(`${SUB.spotifyKey}.clear`, { user_id: userId }, 3000);
    if (r.error) return { ok: false, error: r.error };
    return { ok: true };
  }

  async function skip(): Promise<SpotifyResult> {
    const g = await grant();
    if (g.scopes.length > 0 && !g.scopes.includes(PLAYBACK_SCOPE)) return { ok: false, missingSpotifyScope: true };
    const r = await rpcReply<{ error?: string; code?: string }>(
      `${SUB.gossip}.spotify.next`,
      { channel_id: userId },
      8000
    );
    if (!r.error) return { ok: true };
    return { ok: false, missingSpotifyScope: r.code === 'scope', code: r.code ?? 'failed' };
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
    disconnect,
    skip
  };
}
