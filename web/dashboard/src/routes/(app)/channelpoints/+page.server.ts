// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import type { ChannelPointReward } from '@bagel/kit';
import { COUNTER_SCOPES, REWARD_ACTIONS, REWARD_ON_REDEEM } from '@bagel/kit';
import {
  readRewards,
  createReward,
  updateReward,
  deleteReward,
  setChannelPointsEnabled
} from '$lib/server/channelpoints-store';
import { moduleLoad } from '$lib/server/module-page';
import { done, storeMutator } from '$lib/server/reward-action';
import { flag, hexOrEmpty, int, oneOf, text } from '$lib/server/reward-form';
import { REWARD_COST_MAX, REWARD_COST_MIN, isValidRewardTitle, normalizeCounterName } from '$lib/reward-fields';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

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

type Normalisers = { [K in keyof ChannelPointReward]: (v: unknown) => ChannelPointReward[K] };

const trimmed = text(Infinity, true);

const NORMALISERS: Normalisers = {
  id: text(),
  title: trimmed,
  cost: int(REWARD_COST_MIN, REWARD_COST_MAX, 100),
  prompt: text(200),
  backgroundColor: hexOrEmpty,
  isEnabled: flag(true),
  isPaused: flag(false),
  isUserInputRequired: flag(false),
  maxPerStreamEnabled: flag(false),
  maxPerStream: int(1, 1_000_000, 1),
  maxPerUserPerStreamEnabled: flag(false),
  maxPerUserPerStream: int(1, 1_000_000, 1),
  globalCooldownEnabled: flag(false),
  globalCooldownSeconds: int(1, 604_800, 60),
  action: oneOf(REWARD_ACTIONS, 'none'),
  message: text(400),
  onRedeem: oneOf(REWARD_ON_REDEEM, 'leave'),
  counter: (v) => normalizeCounterName(trimmed(v)),
  counterScope: oneOf(COUNTER_SCOPES, 'viewer_command'),
  points: int(0, 100_000_000, 0),
  liveOnly: flag(false)
};

function normaliseReward(obj: Record<string, unknown>): ChannelPointReward {
  const out: Record<string, unknown> = {};
  for (const key of Object.keys(NORMALISERS) as (keyof ChannelPointReward)[]) {
    out[key] = NORMALISERS[key](obj[key]);
  }
  return out as unknown as ChannelPointReward;
}

function parseReward(raw: string): ChannelPointReward | null {
  let obj: Record<string, unknown>;
  try {
    obj = JSON.parse(raw) as Record<string, unknown>;
  } catch {
    return null;
  }
  const reward = normaliseReward(obj);
  return isValidRewardTitle(reward.title) ? reward : null;
}

function parseRewardForm(form: FormData): ChannelPointReward | null {
  const draft = parseReward(String(form.get('reward') ?? ''));
  if (!draft) return null;
  if (form.get('counter_enabled') !== 'true') return draft;
  if (!draft.counter) return null;
  return draft;
}

const mutate = storeMutator('channelpoints', DEMO, (uid) => uid);

export const actions: Actions = {
  create: mutate('create', 'Invalid reward.', async (uid, f) => {
    const draft = parseRewardForm(f);
    if (!draft) return null;
    return done(await createReward(uid, draft), draft.title);
  }),

  update: mutate('update', 'Invalid reward.', async (uid, f) => {
    const draft = parseRewardForm(f);
    if (!draft) return null;
    if (!draft.id) return null;
    return done(await updateReward(uid, draft), draft.title);
  }),

  delete: mutate('delete', 'Missing reward id.', async (uid, f) => {
    const id = String(f.get('id') ?? '');
    if (!id) return null;
    return done(await deleteReward(uid, id), id);
  }),

  toggle: mutate('toggle', 'Invalid reward.', async (uid, f) => {
    const enabled = f.get('is_enabled') === 'on';
    await setChannelPointsEnabled(uid, enabled);
    return String(enabled);
  })
};
