// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { ChannelPointReward, CounterScope, RewardActionKind, RewardOnRedeem } from '@bagel/shared';
import { clampInt, COUNTER_SCOPES, REWARD_ACTIONS, REWARD_ON_REDEEM } from '@bagel/shared';
import {
  readRewards,
  createReward,
  updateReward,
  deleteReward,
  setChannelPointsEnabled,
  type RewardResult
} from '$lib/server/channelpoints-store';
import { moduleLoad } from '$lib/server/module-page';
import { moduleAction, type ModuleMutation } from '$lib/server/module-action';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

export const load: PageServerLoad = ({ locals }) =>
  moduleLoad('channelpoints', locals.session, {
    demo: DEMO
      ? async () => ({ enabled: true, rewards: (await import('$lib/server/demo-data')).demoRewards() })
      : undefined,
    read: async (uid) => {
      const view = await readRewards(uid);
      return { enabled: view.enabled, rewards: view.rewards };
    },
    blank: () => ({ enabled: false, rewards: [] as ChannelPointReward[] })
  });

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
// Returned from the verb rather than thrown: it is a reason the broadcaster
// acts on, not a fault, so it must not become moduleAction's generic line.
function resultFail(r: Extract<RewardResult, { ok: false }>) {
  if (r.missingScope) return fail(403, { ok: false, missingScope: true });
  return fail(400, { ok: false, error: r.error ?? 'failed' });
}

// mutate binds one POST action to the module write skeleton
// ($lib/server/module-action): delegate gate, form, demo short-circuit, error
// mapping, audit. Each verb below is only its own parse plus its store call.
function mutate(op: string, invalid: string, run: ModuleMutation) {
  return moduleAction('channelpoints', op, run, { demo: DEMO, invalid });
}

export const actions: Actions = {
  create: mutate('create', 'Invalid reward.', async (uid, f) => {
    const draft = parseReward(String(f.get('reward') ?? ''));
    if (!draft) return null;
    const res = await createReward(uid, draft);
    return res.ok ? draft.title : resultFail(res);
  }),

  update: mutate('update', 'Invalid reward.', async (uid, f) => {
    const draft = parseReward(String(f.get('reward') ?? ''));
    if (!draft || !draft.id) return null;
    const res = await updateReward(uid, draft);
    return res.ok ? draft.title : resultFail(res);
  }),

  delete: mutate('delete', 'Missing reward id.', async (uid, f) => {
    const id = String(f.get('id') ?? '');
    if (!id) return null;
    const res = await deleteReward(uid, id);
    return res.ok ? id : resultFail(res);
  }),

  // Master on/off for whether the bot acts on redemptions at all.
  toggle: mutate('toggle', 'Invalid reward.', async (uid, f) => {
    const enabled = f.get('is_enabled') === 'on';
    await setChannelPointsEnabled(uid, enabled);
    return String(enabled);
  })
};
