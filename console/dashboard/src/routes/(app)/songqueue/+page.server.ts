// Copyright (c) 2026 Adam Ousmer. All rights reserved.

import type { Actions, PageServerLoad } from './$types';
import { SPOTIFY_SR_PERMS, SPOTIFY_QUOTA_TIERS, blankSpotifySr, blankSpotifyRedeem, blankSpotifyQuotas } from '@bagel/shared';
import type {
  RewardDraft,
  RewardOnRedeem,
  SpotifyResult,
  SpotifyStore,
  SpotifySrPerm
} from '$lib/server/spotify-store';
import { spotifyStore } from '$lib/server/spotify-store';
import { spotifyRedirectURI, spotifyScopeGap, spotifyConfigured } from '$lib/server/oauth';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
import { getSongQueue, type SongQueueDoc } from '@bagel/shared/server/songqueue-store';
import { gateModulePage } from '$lib/server/module-gate';
import type { Session } from '$lib/server/session';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// Delegate scope comes from the spotify catalog def (see module-gate.ts).
function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'songqueue');
}

export const load: PageServerLoad = async ({ locals, url }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);

  if (DEMO) {
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

  // OAuth round-trip notices ride query params (?connected=1 / ?e=slug); read
  // and drop them into data so the page can show a one-shot banner.
  const justConnected = url.searchParams.get('connected') === '1';
  const rawSlug = url.searchParams.get('e') ?? '';
  const errorSlug = ['state', 'oauth', 'notoken', 'noapp', 'unconfigured', 'store'].includes(rawSlug)
    ? rawSlug
    : '';

  try {
    const store = spotifyStore(uid);
    // The module blob and the connection presence are independent reads; run
    // them together so SSR is one round trip deep.
    const [view, grant, app, queue] = await Promise.all([
      store.read(),
      store.grant(),
      store.app(),
      getSongQueue(uid)
    ]);
    // The callback URL is fleet-wide and not a secret: the page shows it so a
    // broadcaster can register it on their own Spotify app, which Spotify then
    // matches byte-for-byte at both ends of the flow. A missing
    // SPOTIFY_REDIRECT_URI is a deploy gap, not a backend outage — surface it
    // as unconfigured rather than collapsing the page behind the degraded
    // banner (that is how a forgotten Doppler key looked like "could not
    // reach the backend" on first ship of BYO Spotify apps).
    //
    // scopeGap is resolved here rather than in the browser: the scope set the
    // flow asks for is server config (DASHBOARD_SPOTIFY_SCOPES), and the page
    // only needs the answer: is this grant short, and of what.
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
  } catch {
    return {
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
      errorSlug: '',
      degraded: true
    };
  }
};

// QueueView is the display slice of sesame's queue doc: titles, artists as
// one line, and who asked. Track ids and timestamps stay server-side; the
// page has no use for them.
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

function requireSession(locals: App.Locals): string | null {
  if (!DEMO && !locals.session) return null;
  return effectiveId(locals.session);
}

// resultFail maps a store failure to a SvelteKit fail(): a missing-scope
// rejection carries a flag so the page shows the reconnect CTA.
function resultFail(r: Extract<SpotifyResult, { ok: false }>) {
  if (r.missingScope) return fail(403, { ok: false, missingScope: true });
  return fail(400, { ok: false, error: r.error ?? 'failed' });
}

// run is the shared action skeleton: gate, resolve the session, short-circuit
// in demo, then run the store operation with uniform error handling + audit.
async function run(
  locals: App.Locals,
  audit: { action: string; detail: string },
  work: (store: SpotifyStore) => Promise<SpotifyResult>
) {
  gate(locals.session);
  const uid = requireSession(locals);
  if (uid === null) return fail(401, { ok: false, error: 'Not signed in.' });
  if (DEMO) return { ok: true };

  let res: SpotifyResult;
  try {
    res = await work(spotifyStore(uid));
  } catch (e) {
    logger.error({ err: e }, `[spotify] ${audit.action} failed`);
    return fail(400, { ok: false });
  }
  if (!res.ok) return resultFail(res);
  auditDashboardImpersonation(locals.session, audit.action, audit.detail);
  return { ok: true };
}

function asPerm(v: FormDataEntryValue | null): SpotifySrPerm {
  return SPOTIFY_SR_PERMS.includes(v as SpotifySrPerm) ? (v as SpotifySrPerm) : 'everyone';
}

function asOnRedeem(v: FormDataEntryValue | null): RewardOnRedeem {
  return v === 'cancel' || v === 'leave' ? v : 'fulfill';
}

// parseRewardDraft validates the reward + behaviour fields into a draft, or a
// user-facing error message.
// Each field states its own rule and returns either the value or the message
// the form shows. Collected below, so parseRewardDraft reads as the list of
// fields rather than as an interleaving of parsing and validation.
type Checked<T> = { value: T } | { error: string };

function checkTitle(f: FormData): Checked<string> {
  const title = String(f.get('title') ?? '').trim();
  if (!title || title.length > 45) return { error: 'Title is required (max 45 characters).' };
  return { value: title };
}

// Twitch's own bounds for a channel-point reward.
const COST_MIN = 1;
const COST_MAX = 10_000_000;

function checkCost(f: FormData): Checked<number> {
  const cost = Math.trunc(Number(f.get('cost')));
  // One reason per line. The three ways a cost can be unusable share a message
  // but not a test, and folding them into one condition only hides that.
  if (!Number.isFinite(cost)) return { error: 'Enter a valid point cost.' };
  if (cost < COST_MIN) return { error: 'Enter a valid point cost.' };
  if (cost > COST_MAX) return { error: 'Enter a valid point cost.' };
  return { value: cost };
}

// Reward tile colour: a "#rrggbb" hex, or blank for Twitch's default.
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

// Global cooldown in seconds; 0 disables. Twitch caps it at 604800s (one week).
// Clamped rather than rejected: any number the form can produce has a sensible
// nearest legal value, so there is nothing to tell the author about.
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
  // saveApp takes the broadcaster's OWN Spotify application. The secret is
  // write-only from here: it goes straight into sealed custody and is never
  // read back into the console (see spotify-store's SpotifyApp).
  saveApp: async ({ request, locals }) => {
    const f = await request.formData();
    const clientId = String(f.get('client_id') ?? '').trim();
    const clientSecret = String(f.get('client_secret') ?? '').trim();
    if (!clientId || !clientSecret) {
      return fail(400, { ok: false, error: 'Both the client ID and the client secret are required.' });
    }
    // The audit detail records WHICH app, never the secret.
    return run(locals, { action: 'spotify:app', detail: clientId }, (s) => s.saveApp(clientId, clientSecret));
  },

  clearApp: ({ locals }) => run(locals, { action: 'spotify:app_clear', detail: '' }, (s) => s.clearApp()),

  toggle: async ({ request, locals }) => {
    const enabled = (await request.formData()).get('is_enabled') === 'on';
    return run(locals, { action: 'spotify:toggle', detail: String(enabled) }, (s) => s.setEnabled(enabled));
  },

  sr: async ({ request, locals }) => {
    const f = await request.formData();
    const sr = {
      enabled: f.get('sr_enabled') === 'on',
      perm: asPerm(f.get('perm')),
      allowOffline: f.get('sr_allow_offline') === 'on'
    };
    return run(
      locals,
      { action: 'spotify:sr', detail: `${sr.enabled}/${sr.perm}/off=${sr.allowOffline}` },
      (s) => s.saveSr(sr)
    );
  },

  quotas: async ({ request, locals }) => {
    const f = await request.formData();
    // Empty or non-positive input means unlimited for that tier; the store
    // coerces the same way on read, so both directions agree on what null is.
    const quotas = blankSpotifyQuotas();
    for (const tier of SPOTIFY_QUOTA_TIERS) {
      const n = Number(f.get(`quota_${tier}`));
      quotas[tier] = Number.isFinite(n) && n > 0 ? Math.floor(n) : null;
    }
    const detail = SPOTIFY_QUOTA_TIERS.map((t) => `${t}=${quotas[t] ?? 'inf'}`).join('/');
    return run(locals, { action: 'spotify:quotas', detail }, (s) => s.saveQuotas(quotas));
  },

  redeemToggle: async ({ request, locals }) => {
    const f = await request.formData();
    const path = {
      enabled: f.get('redeem_enabled') === 'on',
      allowOffline: f.get('redeem_allow_offline') === 'on'
    };
    return run(
      locals,
      { action: 'spotify:redeem_toggle', detail: `${path.enabled}/off=${path.allowOffline}` },
      (s) => s.setRedeemPath(path)
    );
  },

  saveReward: async ({ request, locals }) => {
    const parsed = parseRewardDraft(await request.formData());
    if ('error' in parsed) return fail(400, { ok: false, error: parsed.error });
    return run(locals, { action: 'spotify:reward', detail: parsed.draft.title }, (s) => s.saveReward(parsed.draft));
  },

  deleteReward: ({ locals }) =>
    run(locals, { action: 'spotify:reward_delete', detail: '' }, (s) => s.deleteReward()),

  disconnect: ({ locals }) => run(locals, { action: 'spotify:disconnect', detail: '' }, (s) => s.disconnect())
};
