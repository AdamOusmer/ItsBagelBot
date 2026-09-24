// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { ChannelPointReward, CounterScope, RewardActionKind, RewardOnRedeem } from '@bagel/kit';
import { clampInt, COUNTER_SCOPES, REWARD_ACTIONS, REWARD_ON_REDEEM } from '@bagel/kit';
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

const TWITCH_REWARD_TITLE_MAX_CHARS = 45;

function parseReward(raw: string): ChannelPointReward | null {
  let obj: Partial<ChannelPointReward>;
  try {
    obj = JSON.parse(raw) as Partial<ChannelPointReward>;
  } catch {
    return null;
  }
  const title = String(obj.title ?? '').trim();
  if (!title || title.length > TWITCH_REWARD_TITLE_MAX_CHARS) return null;

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
    counter: String(obj.counter ?? '').trim().replace(/^!/, '').toLowerCase().slice(0, 64),
    counterScope: COUNTER_SCOPES.includes(obj.counterScope as CounterScope)
      ? (obj.counterScope as CounterScope)
      : 'viewer_command',
    points: clampInt(obj.points, 0, 100_000_000, 0),
    liveOnly: obj.liveOnly === true
  };
}

function parseRewardForm(form: FormData): ChannelPointReward | null {
  const draft = parseReward(String(form.get('reward') ?? ''));
  if (!draft) return null;
  if (form.get('counter_enabled') !== 'true') return draft;
  if (!draft.counter) return null;
  return draft;
}

function resultFail(r: Extract<RewardResult, { ok: false }>) {
  if (r.missingScope) return fail(403, { ok: false, missingScope: true });
  if (r.duplicateTitle) return fail(409, { ok: false, duplicateTitle: true });
  return fail(400, { ok: false, error: r.error ?? 'failed' });
}

function mutate(op: string, invalid: string, run: ModuleMutation) {
  return moduleAction('channelpoints', op, run, { demo: DEMO, invalid });
}

export const actions: Actions = {
  create: mutate('create', 'Invalid reward.', async (uid, f) => {
    const draft = parseRewardForm(f);
    if (!draft) return null;
    const res = await createReward(uid, draft);
    return res.ok ? draft.title : resultFail(res);
  }),

  update: mutate('update', 'Invalid reward.', async (uid, f) => {
    const draft = parseRewardForm(f);
    if (!draft) return null;
    if (!draft.id) return null;
    const res = await updateReward(uid, draft);
    return res.ok ? draft.title : resultFail(res);
  }),

  delete: mutate('delete', 'Missing reward id.', async (uid, f) => {
    const id = String(f.get('id') ?? '');
    if (!id) return null;
    const res = await deleteReward(uid, id);
    return res.ok ? id : resultFail(res);
  }),

  toggle: mutate('toggle', 'Invalid reward.', async (uid, f) => {
    const enabled = f.get('is_enabled') === 'on';
    await setChannelPointsEnabled(uid, enabled);
    return String(enabled);
  })
};
