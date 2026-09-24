// Copyright (c) 2026 Adam Ousmer. All rights reserved.

import type { Actions, PageServerLoad } from './$types';
import { SPOTIFY_SR_PERMS, SPOTIFY_QUOTA_TIERS, blankSpotifySr, blankSpotifyRedeem, blankSpotifyQuotas } from '@bagel/kit';
import type {
  RewardDraft,
  RewardOnRedeem,
  SpotifyResult,
  SpotifyStore,
  SpotifySrPerm
} from '$lib/server/spotify-store';
import { spotifyStore } from '$lib/server/spotify-store';
import { spotifyRedirectURI, spotifyScopeGap, spotifyConfigured } from '$lib/server/oauth';
import { getSongQueue, type SongQueueDoc } from '@bagel/kit/server/songqueue-store';
import { moduleLoad } from '$lib/server/module-page';
import { moduleAction } from '$lib/server/module-action';
import type { MutationRefusal } from '@bagel/kit/server/form-action';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

const DEMO = dev && env.DEMO === '1';

export const load: PageServerLoad = ({ locals, url }) => {
  const justConnected = url.searchParams.get('connected') === '1';
  const rawSlug = url.searchParams.get('e') ?? '';
  const errorSlug = ['state', 'oauth', 'notoken', 'noapp', 'unconfigured', 'store'].includes(rawSlug)
    ? rawSlug
    : '';

  return moduleLoad('songqueue', locals.session, {
    demo: DEMO
      ? async () => {
          const { demoSpotifyView } = await import('$lib/server/demo-data');
          return {
            ...demoSpotifyView(),
            quotas: blankSpotifyQuotas(),
            queue: {
              current: { title: 'Mr. Brightside', artists: 'The Killers', requester: 'alice' },
              up: [
                { title: 'Human', artists: 'The Killers', requester: 'bob' },
                { title: 'Somebody Told Me', artists: 'The Killers', requester: 'carol' }
              ]
            } as QueueView,
            connected: true,
            scopeGap: [] as string[],
            app: { present: true, clientId: 'demo-client-id' },
            redirectUri: 'https://console.example/spotify/callback',
            justConnected: false,
            errorSlug: ''
          };
        }
      : undefined,
    read: async (uid) => {
      const store = spotifyStore(uid);
      const [view, grant, app, queue] = await Promise.all([
        store.read(),
        store.grant(),
        store.app(),
        getSongQueue(uid)
      ]);
      const redirectUri = spotifyConfigured() ? spotifyRedirectURI() : '';
      return {
        ...view,
        queue: shapeQueue(queue),
        connected: grant.connected,
        scopeGap: grant.connected ? spotifyScopeGap(grant.scopes) : [],
        app,
        redirectUri,
        justConnected,
        errorSlug: errorSlug || (!redirectUri ? 'unconfigured' : '')
      };
    },
    blank: () => ({
      enabled: false,
      sr: blankSpotifySr(),
      redeem: blankSpotifyRedeem(),
      quotas: blankSpotifyQuotas(),
      queue: { current: null, up: [] } as QueueView,
      connected: false,
      scopeGap: [] as string[],
      app: { present: false, clientId: '' },
      redirectUri: '',
      justConnected: false,
      errorSlug: ''
    })
  });
};

export interface QueueView {
  current: QueueRow | null;
  up: QueueRow[];
}
interface QueueRow {
  title: string;
  artists: string;
  requester: string;
}

function shapeQueue(doc: SongQueueDoc): QueueView {
  const row = (e: NonNullable<SongQueueDoc['current']>): QueueRow => ({
    title: e.title,
    artists: (e.artists ?? []).join(', '),
    requester: e.req_name
  });
  return {
    current: doc.current ? row(doc.current) : null,
    up: (doc.up ?? []).slice(0, 10).map(row)
  };
}

function resultFail(r: Extract<SpotifyResult, { ok: false }>) {
  if (r.missingScope) return fail(403, { ok: false, missingScope: true });
  return fail(400, { ok: false, error: r.error ?? 'failed' });
}

function mutate(
  op: string,
  invalid: string,
  run: (store: SpotifyStore, f: FormData) => Promise<string | null | MutationRefusal>
) {
  return moduleAction('songqueue', op, (uid, f) => run(spotifyStore(uid), f), {
    demo: DEMO,
    invalid,
    auditAs: 'spotify'
  });
}

function done(res: SpotifyResult, detail: string): string | MutationRefusal {
  return res.ok ? detail : resultFail(res);
}

function asPerm(v: FormDataEntryValue | null): SpotifySrPerm {
  return SPOTIFY_SR_PERMS.includes(v as SpotifySrPerm) ? (v as SpotifySrPerm) : 'everyone';
}

function asOnRedeem(v: FormDataEntryValue | null): RewardOnRedeem {
  return v === 'cancel' || v === 'leave' ? v : 'fulfill';
}

type Checked<T> = { value: T } | { error: string };

function checkTitle(f: FormData): Checked<string> {
  const title = String(f.get('title') ?? '').trim();
  if (!title || title.length > 45) return { error: 'Title is required (max 45 characters).' };
  return { value: title };
}

const COST_MIN = 1;
const COST_MAX = 10_000_000;

function checkCost(f: FormData): Checked<number> {
  const cost = Math.trunc(Number(f.get('cost')));
  if (!Number.isFinite(cost)) return { error: 'Enter a valid point cost.' };
  if (cost < COST_MIN) return { error: 'Enter a valid point cost.' };
  if (cost > COST_MAX) return { error: 'Enter a valid point cost.' };
  return { value: cost };
}

function checkColor(f: FormData): Checked<string> {
  const color = String(f.get('color') ?? '').trim();
  if (color && !/^#[0-9a-fA-F]{6}$/.test(color)) return { error: 'Pick a valid colour.' };
  return { value: color };
}

function checkReply(f: FormData): Checked<string> {
  const replyMessage = String(f.get('replyMessage') ?? '').trim();
  if (replyMessage.length > 200) return { error: 'Reply is too long (max 200 characters).' };
  return { value: replyMessage };
}

function readCooldown(f: FormData): number {
  const raw = Math.trunc(Number(f.get('cooldown') ?? 0));
  return Number.isFinite(raw) ? Math.min(Math.max(raw, 0), 604_800) : 0;
}

function parseRewardDraft(f: FormData): { draft: RewardDraft } | { error: string } {
  const title = checkTitle(f);
  if ('error' in title) return title;
  const cost = checkCost(f);
  if ('error' in cost) return cost;
  const color = checkColor(f);
  if ('error' in color) return color;
  const replyMessage = checkReply(f);
  if ('error' in replyMessage) return replyMessage;

  return {
    draft: {
      title: title.value,
      cost: cost.value,
      onRedeem: asOnRedeem(f.get('onRedeem')),
      color: color.value,
      cooldown: readCooldown(f),
      replyMessage: replyMessage.value
    }
  };
}

export const actions: Actions = {
  saveApp: mutate('app', 'Both the client ID and the client secret are required.', async (store, f) => {
    const clientId = String(f.get('client_id') ?? '').trim();
    const clientSecret = String(f.get('client_secret') ?? '').trim();
    if (!clientId || !clientSecret) return null;
    return done(await store.saveApp(clientId, clientSecret), clientId);
  }),

  clearApp: mutate('app_clear', 'Invalid request.', async (store) => done(await store.clearApp(), '')),

  toggle: mutate('toggle', 'Invalid request.', async (store, f) => {
    const enabled = f.get('is_enabled') === 'on';
    return done(await store.setEnabled(enabled), String(enabled));
  }),

  sr: mutate('sr', 'Invalid request.', async (store, f) => {
    const sr = {
      enabled: f.get('sr_enabled') === 'on',
      perm: asPerm(f.get('perm')),
      allowOffline: f.get('sr_allow_offline') === 'on'
    };
    return done(await store.saveSr(sr), `${sr.enabled}/${sr.perm}/off=${sr.allowOffline}`);
  }),

  quotas: mutate('quotas', 'Invalid request.', async (store, f) => {
    const quotas = blankSpotifyQuotas();
    for (const tier of SPOTIFY_QUOTA_TIERS) {
      const n = Number(f.get(`quota_${tier}`));
      quotas[tier] = Number.isFinite(n) && n > 0 ? Math.floor(n) : null;
    }
    const detail = SPOTIFY_QUOTA_TIERS.map((t) => `${t}=${quotas[t] ?? 'inf'}`).join('/');
    return done(await store.saveQuotas(quotas), detail);
  }),

  redeemToggle: mutate('redeem_toggle', 'Invalid request.', async (store, f) => {
    const path = {
      enabled: f.get('redeem_enabled') === 'on',
      allowOffline: f.get('redeem_allow_offline') === 'on'
    };
    return done(await store.setRedeemPath(path), `${path.enabled}/off=${path.allowOffline}`);
  }),

  saveReward: mutate('reward', 'Invalid reward.', async (store, f) => {
    const parsed = parseRewardDraft(f);
    if ('error' in parsed) return fail(400, { ok: false, error: parsed.error });
    return done(await store.saveReward(parsed.draft), parsed.draft.title);
  }),

  deleteReward: mutate('reward_delete', 'Invalid request.', async (store) => done(await store.deleteReward(), '')),

  disconnect: mutate('disconnect', 'Invalid request.', async (store) => done(await store.disconnect(), ''))
};
