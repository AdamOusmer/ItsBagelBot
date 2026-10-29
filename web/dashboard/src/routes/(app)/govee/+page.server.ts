// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { GOVEE_COLOR_NAMES } from '@bagel/kit';
import { goveeStore, type GoveeDevice, type GoveeView, type RewardDraft } from '$lib/server/govee-store';
import { moduleLoad } from '$lib/server/module-page';
import { done, storeMutator } from '$lib/server/reward-action';
import { parseRewardCore } from '$lib/server/reward-form';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { fail } from '@sveltejs/kit';

const DEMO = dev && env.DEMO === '1';

type DeviceList = { devices: GoveeDevice[]; error?: string; code?: 'key_rejected' | 'unreachable' };
type GoveePage = {
  enabled: boolean;
  keyPresent: boolean;
  setupPreview?: boolean;
  bindings: GoveeView['bindings'];
  devices: DeviceList | Promise<DeviceList>;
  colors: string[];
};

export const load: PageServerLoad = ({ locals, url }) => {
  const colors = [...GOVEE_COLOR_NAMES];
  return moduleLoad<GoveePage>('govee', locals.session, {
    demo: DEMO
      ? async () => {
          const { demoGoveeView, demoGoveeDevices } = await import('$lib/server/demo-data');
          const setupPreview = url.searchParams.get('setup') === '1';
          return { ...demoGoveeView(), keyPresent: !setupPreview, setupPreview, devices: { devices: demoGoveeDevices(), error: undefined }, colors };
        }
      : undefined,
    read: async (uid) => {
      const store = goveeStore(uid);
      const view = await store.read();
      const devices = view.keyPresent
        ? store.listDevices()
        : Promise.resolve({ devices: [] as GoveeDevice[], error: undefined });
      return { ...view, devices, colors };
    },
    blank: () => ({
      enabled: false,
      keyPresent: false,
      bindings: [] as GoveeView['bindings'],
      devices: { devices: [] as GoveeDevice[], error: undefined },
      colors
    })
  });
};

function parseRewardDraft(f: FormData): { draft: RewardDraft } | { error: string } {
  const core = parseRewardCore(f);
  if ('error' in core) return core;
  return { draft: { ...core.draft, allowOff: f.get('allow_off') === 'on', allowOffline: f.get('allow_offline') === 'on' } };
}

function parseRewardForm(f: FormData): { device: GoveeDevice; draft: RewardDraft } | { error: string } {
  const device = String(f.get('device') ?? '').trim();
  const sku = String(f.get('sku') ?? '').trim();
  if (!device || !sku) return { error: 'Pick a light first.' };

  const parsed = parseRewardDraft(f);
  if ('error' in parsed) return parsed;
  return { device: { device, sku, name: String(f.get('deviceName') ?? '').trim(), color: true }, draft: parsed.draft };
}

const mutate = storeMutator('govee', DEMO, goveeStore);

export const actions: Actions = {
  saveKey: async (event) => {
    let unchecked = false;
    const result = await mutate('key_set', 'Enter your Govee API key.', async (store, f) => {
      const key = String(f.get('key') ?? '').trim();
      if (!key) return null;
      const res = await store.setKey(key);
      unchecked = !res.ok && res.code === 'key_unchecked';
      return unchecked ? '' : done(res, '');
    })(event);
    return 'ok' in result && unchecked ? { ...result, code: 'key_unchecked' } : result;
  },

  clearKey: mutate('key_clear', 'Invalid request.', async (store) => done(await store.clearKey(), '')),

  saveReward: mutate('reward', 'Pick a light first.', async (store, f) => {
    const parsed = parseRewardForm(f);
    if ('error' in parsed) return fail(400, { ok: false, error: parsed.error });
    return done(await store.saveReward(parsed.device, parsed.draft), parsed.draft.title);
  }),

  deleteReward: mutate('reward_delete', 'Pick a light first.', async (store, f) => {
    const deviceId = String(f.get('device') ?? '').trim();
    if (!deviceId) return null;
    return done(await store.deleteReward(deviceId), deviceId);
  }),

  toggle: mutate('toggle', 'Invalid request.', async (store, f) => {
    const enabled = f.get('is_enabled') === 'on';
    return done(await store.setEnabled(enabled), String(enabled));
  })
};
