// Copyright (c) 2026 Adam Ousmer. All rights reserved.

import type { Actions, PageServerLoad } from './$types';
import { SPOTIFY_SR_PERMS } from '@bagel/shared';
import type {
  RewardDraft,
  RewardOnRedeem,
  SpotifyResult,
  SpotifyStore,
  SpotifySrPerm
} from '$lib/server/spotify-store';
import { spotifyStore } from '$lib/server/spotify-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import { logger } from '@bagel/shared/server/logger';
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
    return { ...demoSpotifyView(), connected: true, justConnected: false, errorSlug: '' };
  }

  // OAuth round-trip notices ride query params (?connected=1 / ?e=slug); read
  // and drop them into data so the page can show a one-shot banner.
  const justConnected = url.searchParams.get('connected') === '1';
  const rawSlug = url.searchParams.get('e') ?? '';
  const errorSlug = ['state', 'oauth', 'notoken', 'unconfigured', 'store'].includes(rawSlug) ? rawSlug : '';

  try {
    const store = spotifyStore(uid);
    // The module blob and the connection presence are independent reads; run
    // them together so SSR is one round trip deep.
    const [view, connected] = await Promise.all([store.read(), store.connected()]);
    return { ...view, connected, justConnected, errorSlug };
  } catch {
    return {
      enabled: false,
      sr: { enabled: false, perm: 'everyone' as SpotifySrPerm },
      redeem: { enabled: false, rewardId: '', onRedeem: 'fulfill' as RewardOnRedeem, replyMessage: '', reward: null },
      connected: false,
      justConnected: false,
      errorSlug: '',
      degraded: true
    };
  }
};

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
function parseRewardDraft(f: FormData): { draft: RewardDraft } | { error: string } {
  const title = String(f.get('title') ?? '').trim();
  if (!title || title.length > 45) return { error: 'Title is required (max 45 characters).' };

  const cost = Math.trunc(Number(f.get('cost')));
  if (!Number.isFinite(cost)) return { error: 'Enter a valid point cost.' };
  if (cost < 1 || cost > 10_000_000) return { error: 'Enter a valid point cost.' };

  // Reward tile colour: a "#rrggbb" hex, or blank for Twitch's default.
  const color = String(f.get('color') ?? '').trim();
  if (color && !/^#[0-9a-fA-F]{6}$/.test(color)) return { error: 'Pick a valid colour.' };

  const replyMessage = String(f.get('replyMessage') ?? '').trim();
  if (replyMessage.length > 200) return { error: 'Reply is too long (max 200 characters).' };

  // Global cooldown in seconds; 0 disables. Twitch caps it at 604800s (one week).
  const rawCooldown = Math.trunc(Number(f.get('cooldown') ?? 0));
  const cooldown = Number.isFinite(rawCooldown) ? Math.min(Math.max(rawCooldown, 0), 604_800) : 0;

  return {
    draft: {
      title,
      cost,
      onRedeem: asOnRedeem(f.get('onRedeem')),
      color,
      cooldown,
      replyMessage
    }
  };
}

export const actions: Actions = {
  toggle: async ({ request, locals }) => {
    const enabled = (await request.formData()).get('is_enabled') === 'on';
    return run(locals, { action: 'spotify:toggle', detail: String(enabled) }, (s) => s.setEnabled(enabled));
  },

  sr: async ({ request, locals }) => {
    const f = await request.formData();
    const sr = { enabled: f.get('sr_enabled') === 'on', perm: asPerm(f.get('perm')) };
    return run(locals, { action: 'spotify:sr', detail: `${sr.enabled}/${sr.perm}` }, (s) => s.saveSr(sr));
  },

  redeemToggle: async ({ request, locals }) => {
    const enabled = (await request.formData()).get('redeem_enabled') === 'on';
    return run(locals, { action: 'spotify:redeem_toggle', detail: String(enabled) }, (s) =>
      s.setRedeemEnabled(enabled)
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
