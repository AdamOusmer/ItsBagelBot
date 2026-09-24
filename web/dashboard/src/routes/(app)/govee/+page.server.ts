// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { GOVEE_COLOR_NAMES } from '@bagel/kit';
import {
  goveeStore,
  type GoveeDevice,
  type GoveeOnRedeem,
  type GoveeResult,
  type GoveeStore,
  type GoveeView,
  type RewardDraft
} from '$lib/server/govee-store';
import { moduleLoad } from '$lib/server/module-page';
import { moduleAction } from '$lib/server/module-action';
import type { MutationRefusal } from '@bagel/kit/server/form-action';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

const DEMO = dev && env.DEMO === '1';

type DeviceList = { devices: GoveeDevice[]; error?: string };
type GoveePage = {
  enabled: boolean;
  keyPresent: boolean;
  bindings: GoveeView['bindings'];
  devices: DeviceList | Promise<DeviceList>;
  colors: string[];
};

export const load: PageServerLoad = ({ locals }) => {
  const colors = [...GOVEE_COLOR_NAMES];
  return moduleLoad<GoveePage>('govee', locals.session, {
    demo: DEMO
      ? async () => {
          const { demoGoveeView, demoGoveeDevices } = await import('$lib/server/demo-data');
          return { ...demoGoveeView(), devices: { devices: demoGoveeDevices(), error: undefined }, colors };
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

function resultFail(r: Extract<GoveeResult, { ok: false }>) {
  if (r.missingScope) return fail(403, { ok: false, missingScope: true });
  return fail(400, { ok: false, error: r.error ?? 'failed' });
}

function asOnRedeem(v: FormDataEntryValue | null): GoveeOnRedeem {
  return v === 'cancel' || v === 'leave' ? v : 'fulfill';
}

function parseRewardDraft(f: FormData): { draft: RewardDraft } | { error: string } {
  const title = String(f.get('title') ?? '').trim();
  if (!title || title.length > 45) return { error: 'Title is required (max 45 characters).' };

  const cost = Math.trunc(Number(f.get('cost')));
  if (!Number.isFinite(cost)) return { error: 'Enter a valid point cost.' };
  if (cost < 1 || cost > 10_000_000) return { error: 'Enter a valid point cost.' };

  const color = String(f.get('color') ?? '').trim();
  if (color && !/^#[0-9a-fA-F]{6}$/.test(color)) return { error: 'Pick a valid colour.' };

  const replyMessage = String(f.get('replyMessage') ?? '').trim();
  if (replyMessage.length > 200) return { error: 'Reply is too long (max 200 characters).' };

  const rawCooldown = Math.trunc(Number(f.get('cooldown') ?? 0));
  const cooldown = Number.isFinite(rawCooldown) ? Math.min(Math.max(rawCooldown, 0), 604_800) : 0;

  return {
    draft: {
      title,
      cost,
      onRedeem: asOnRedeem(f.get('onRedeem')),
      color,
      cooldown,
      replyMessage,
      allowOff: f.get('allow_off') === 'on',
      allowOffline: f.get('allow_offline') === 'on'
    }
  };
}

function parseRewardForm(f: FormData): { device: GoveeDevice; draft: RewardDraft } | { error: string } {
  const device = String(f.get('device') ?? '').trim();
  const sku = String(f.get('sku') ?? '').trim();
  if (!device || !sku) return { error: 'Pick a light first.' };

  const parsed = parseRewardDraft(f);
  if ('error' in parsed) return parsed;
  return { device: { device, sku, name: String(f.get('deviceName') ?? '').trim(), color: true }, draft: parsed.draft };
}

function mutate(
  op: string,
  invalid: string,
  run: (store: GoveeStore, f: FormData) => Promise<string | null | MutationRefusal>
) {
  return moduleAction('govee', op, (uid, f) => run(goveeStore(uid), f), { demo: DEMO, invalid });
}

function done(res: GoveeResult, detail: string): string | MutationRefusal {
  return res.ok ? detail : resultFail(res);
}

export const actions: Actions = {
  saveKey: mutate('key_set', 'Enter your Govee API key.', async (store, f) => {
    const key = String(f.get('key') ?? '').trim();
    if (!key) return null;
    return done(await store.setKey(key), '');
  }),

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
