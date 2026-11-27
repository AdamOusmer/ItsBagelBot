// Copyright (c) 2026 Adam Ousmer. All rights reserved.

import type { Actions, PageServerLoad } from './$types';
import {
  DISABLED_TRIGGERS_KEY,
  SPOTIFY_SR_PERMS,
  SPOTIFY_QUOTA_TIERS,
  blankSpotifySr,
  blankSpotifyRedeem,
  blankSpotifyQuotas
} from '@bagel/kit';
import type { SpotifySrPerm } from '#lib/server/spotify-store';
import { positiveCount, spotifyStore } from '#lib/server/spotify-store';
import { done, storeMutator } from '#lib/server/reward-action';
import { oneOf, parseRewardCore } from '#lib/server/reward-form';
import { spotifyRedirectURI, spotifyScopeGap, spotifyConfigured } from '#lib/server/oauth';
import { getSongQueue } from '@bagel/kit/server/songqueue-store';
import type { QueueView } from '#lib/server/songqueue-view';
import { queueView } from '#lib/server/songqueue-live';
import { moduleLoad } from '#lib/server/module-page';
import { fail } from '@sveltejs/kit';

export const load: PageServerLoad = ({ locals, url }) => {
  const justConnected = url.searchParams.get('connected') === '1';
  const rawSlug = url.searchParams.get('e') ?? '';
  const errorSlug = ['state', 'oauth', 'notoken', 'noapp', 'unconfigured', 'store', 'declined'].includes(rawSlug)
    ? rawSlug
    : '';
  const rawWarning = url.searchParams.get('w') ?? '';
  const connectWarning = ['owner_premium', 'not_registered'].includes(rawWarning) ? rawWarning : '';

  return moduleLoad('songqueue', locals.session, {
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
        queue: await queueView(uid, grant.connected, queue),
        connected: grant.connected,
        scopeGap: grant.connected ? spotifyScopeGap(grant.scopes) : [],
        needsReconnect: grant.connected && grant.needsReconnect,
        app,
        redirectUri,
        justConnected,
        errorSlug: errorSlug || (!redirectUri ? 'unconfigured' : ''),
        connectWarning
      };
    },
    blank: () => ({
      enabled: false,
      sr: blankSpotifySr(),
      redeem: blankSpotifyRedeem(),
      quotas: blankSpotifyQuotas(),
      maxDepth: null,
      disabledTriggers: '',
      queue: { current: null, up: [] } as QueueView,
      connected: false,
      scopeGap: [] as string[],
      needsReconnect: false,
      app: { present: false, clientId: '' },
      redirectUri: '',
      justConnected: false,
      errorSlug: '',
      connectWarning
    })
  });
};

const mutate = storeMutator('songqueue', spotifyStore, 'spotify');

const asPerm = oneOf<SpotifySrPerm>(SPOTIFY_SR_PERMS, 'everyone');

export const actions: Actions = {
  saveApp: mutate('app', "That does not look like a Client ID and Client Secret. Copy both again from your app's Settings page in the Spotify Developer Dashboard, with no spaces.", async (store, f) => {
    const clientId = String(f.get('client_id') ?? '').trim();
    const clientSecret = String(f.get('client_secret') ?? '').trim();
    if (!/^[A-Za-z0-9]{16,128}$/.test(clientId) || !/^[A-Za-z0-9]{16,128}$/.test(clientSecret)) return null;
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
      quotas[tier] = positiveCount(f.get(`quota_${tier}`));
    }
    const maxDepth = positiveCount(f.get('max_depth'));
    const detail = SPOTIFY_QUOTA_TIERS.map((t) => `${t}=${quotas[t] ?? 'inf'}`).join('/');
    return done(await store.saveLimits({ quotas, maxDepth }), `${detail}/queue=${maxDepth ?? 'inf'}`);
  }),

  redeemToggle: mutate('redeem_toggle', 'Invalid request.', async (store, f) => {
    const path = {
      enabled: f.get('redeem_enabled') === 'on',
      allowOffline: f.get('redeem_allow_offline') === 'on'
    };
    return done(await store.setRedeemPath(path), `${path.enabled}/off=${path.allowOffline}`);
  }),

  saveReward: mutate('reward', 'Invalid reward.', async (store, f) => {
    const parsed = parseRewardCore(f);
    if ('error' in parsed) return fail(400, { ok: false, error: parsed.error });
    return done(await store.saveReward(parsed.draft), parsed.draft.title);
  }),

  deleteReward: mutate('reward_delete', 'Invalid request.', async (store) => done(await store.deleteReward(), '')),

  disconnect: mutate('disconnect', 'Invalid request.', async (store) => done(await store.disconnect(), '')),

  skip: mutate('skip', 'Invalid request.', async (store) => done(await store.skip(), '')),

  triggers: mutate('triggers', 'Invalid request.', async (store, f) => {
    const raw = String(f.get(DISABLED_TRIGGERS_KEY) ?? '');
    return done(await store.saveDisabledTriggers(raw), raw || 'all-on');
  })
};
