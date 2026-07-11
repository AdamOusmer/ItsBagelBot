import type { Actions, PageServerLoad } from './$types';
import type { ChannelPointReward, CounterScope, RewardActionKind, RewardOnRedeem } from '@bagel/shared';
import { COUNTER_SCOPES, REWARD_ACTIONS, REWARD_ON_REDEEM, blankReward } from '@bagel/shared';
import {
  readRewards,
  createReward,
  updateReward,
  deleteReward,
  setChannelPointsEnabled,
  type RewardResult
} from '$lib/server/channelpoints-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import type { Session } from '$lib/server/session';
import { env } from '$env/dynamic/private';
import { fail, redirect } from '@sveltejs/kit';

function effectiveId(session: Session | null | undefined): string {
  return session?.delegate_of ?? session?.user_id ?? 'demo';
}

// A delegate needs the 'channelpoints' section; a normal login always may.
function gate(session: Session | null | undefined): void {
  if (session?.delegate_of && !(session.sections ?? []).includes('channelpoints')) {
    throw redirect(302, '/');
  }
}

// Demo rewards so the tab renders without a live backend.
function demoRewards(): ChannelPointReward[] {
  return [
    { ...blankReward(), id: 'demo-1', title: 'Say hi', cost: 100, action: 'chat', message: '{user} says hi! 👋', onRedeem: 'fulfill' },
    {
      ...blankReward(),
      id: 'demo-2',
      title: 'Pick the next map',
      cost: 2500,
      isUserInputRequired: true,
      backgroundColor: '#1f69ff',
      action: 'chat',
      message: '{user} picked the next map: {input}',
      onRedeem: 'fulfill',
      maxPerStreamEnabled: true,
      maxPerStream: 1
    }
  ];
}

export const load: PageServerLoad = async ({ locals }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  if (env.DEMO === '1') return { enabled: true, rewards: demoRewards() };
  try {
    const view = await readRewards(uid);
    return { enabled: view.enabled, rewards: view.rewards };
  } catch {
    return { enabled: false, rewards: [] as ChannelPointReward[], degraded: true };
  }
};

// clampInt coerces a form value into a bounded integer.
function clampInt(raw: unknown, min: number, max: number, dflt: number): number {
  const n = Math.trunc(Number(raw));
  if (!Number.isFinite(n)) return dflt;
  return Math.min(max, Math.max(min, n));
}

// parseReward validates and normalizes the posted reward JSON into a full
// ChannelPointReward. Returns null on anything malformed; the action strings are
// constrained to the known sets so a crafted post cannot inject an unknown kind.
function parseReward(raw: string): ChannelPointReward | null {
  let obj: Partial<ChannelPointReward>;
  try {
    obj = JSON.parse(raw) as Partial<ChannelPointReward>;
  } catch {
    return null;
  }
  const title = String(obj.title ?? '').trim();
  if (!title || title.length > 45) return null; // Twitch caps reward titles at 45 chars

  const action: RewardActionKind = REWARD_ACTIONS.includes(obj.action as RewardActionKind)
    ? (obj.action as RewardActionKind)
    : 'none';
  const onRedeem: RewardOnRedeem = REWARD_ON_REDEEM.includes(obj.onRedeem as RewardOnRedeem)
    ? (obj.onRedeem as RewardOnRedeem)
    : 'leave';

  return {
    id: String(obj.id ?? ''),
    title,
    cost: clampInt(obj.cost, 1, 10_000_000, 100),
    prompt: String(obj.prompt ?? '').slice(0, 200),
    backgroundColor: /^#[0-9a-fA-F]{6}$/.test(String(obj.backgroundColor ?? '')) ? String(obj.backgroundColor) : '',
    isEnabled: obj.isEnabled !== false,
    isPaused: obj.isPaused === true,
    isUserInputRequired: obj.isUserInputRequired === true,
    maxPerStreamEnabled: obj.maxPerStreamEnabled === true,
    maxPerStream: clampInt(obj.maxPerStream, 1, 1_000_000, 1),
    maxPerUserPerStreamEnabled: obj.maxPerUserPerStreamEnabled === true,
    maxPerUserPerStream: clampInt(obj.maxPerUserPerStream, 1, 1_000_000, 1),
    globalCooldownEnabled: obj.globalCooldownEnabled === true,
    globalCooldownSeconds: clampInt(obj.globalCooldownSeconds, 1, 604_800, 60),
    action,
    message: String(obj.message ?? '').slice(0, 400),
    onRedeem,
    // Loyalty hooks. The counter name mirrors sesame's normalization (bare
    // key, lower-cased); points are clamped to the same ceiling as a mod grant.
    counter: String(obj.counter ?? '').trim().replace(/^!/, '').toLowerCase().slice(0, 64),
    counterScope: COUNTER_SCOPES.includes(obj.counterScope as CounterScope)
      ? (obj.counterScope as CounterScope)
      : 'viewer_command',
    points: clampInt(obj.points, 0, 100_000_000, 0),
    liveOnly: obj.liveOnly === true
  };
}

// resultFail maps a store RewardResult failure to a SvelteKit fail(): a
// missing-scope rejection carries a flag so the page shows the reconnect CTA.
function resultFail(r: Extract<RewardResult, { ok: false }>) {
  if (r.missingScope) return fail(403, { ok: false, missingScope: true });
  return fail(400, { ok: false, error: r.error ?? 'failed' });
}

export const actions: Actions = {
  create: async ({ request, locals }) => {
    gate(locals.session);
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const draft = parseReward(String(f.get('reward') ?? ''));
    if (!draft) return fail(400, { ok: false, error: 'Invalid reward.' });
    if (env.DEMO === '1') return { ok: true };

    let res: RewardResult;
    try {
      res = await createReward(uid, draft);
    } catch (e) {
      console.error('[channelpoints] create failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false, error: 'create failed' });
    }
    if (!res.ok) return resultFail(res);
    auditDashboardImpersonation(locals.session, 'channelpoints:create', draft.title);
    return { ok: true };
  },

  update: async ({ request, locals }) => {
    gate(locals.session);
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const draft = parseReward(String(f.get('reward') ?? ''));
    if (!draft || !draft.id) return fail(400, { ok: false, error: 'Invalid reward.' });
    if (env.DEMO === '1') return { ok: true };

    let res: RewardResult;
    try {
      res = await updateReward(uid, draft);
    } catch (e) {
      console.error('[channelpoints] update failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false, error: 'update failed' });
    }
    if (!res.ok) return resultFail(res);
    auditDashboardImpersonation(locals.session, 'channelpoints:update', draft.title);
    return { ok: true };
  },

  delete: async ({ request, locals }) => {
    gate(locals.session);
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const id = String(f.get('id') ?? '');
    if (!id) return fail(400, { ok: false, error: 'Missing reward id.' });
    if (env.DEMO === '1') return { ok: true };

    let res: RewardResult;
    try {
      res = await deleteReward(uid, id);
    } catch (e) {
      console.error('[channelpoints] delete failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false, error: 'delete failed' });
    }
    if (!res.ok) return resultFail(res);
    auditDashboardImpersonation(locals.session, 'channelpoints:delete', id);
    return { ok: true };
  },

  // Master on/off for whether the bot acts on redemptions at all.
  toggle: async ({ request, locals }) => {
    gate(locals.session);
    const uid = effectiveId(locals.session);
    if (env.DEMO !== '1' && !locals.session) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const enabled = f.get('is_enabled') === 'on';
    if (env.DEMO === '1') return { ok: true, enabled };

    try {
      await setChannelPointsEnabled(uid, enabled);
    } catch (e) {
      console.error('[channelpoints] toggle failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false });
    }
    auditDashboardImpersonation(locals.session, 'channelpoints:toggle', String(enabled));
    return { ok: true, enabled };
  }
};
