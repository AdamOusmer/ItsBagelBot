import type { Actions, PageServerLoad } from './$types';
import { GOVEE_COLOR_NAMES } from '@bagel/shared';
import {
  goveeStore,
  type GoveeDevice,
  type GoveeOnRedeem,
  type GoveeResult,
  type GoveeStore,
  type GoveeView,
  type RewardDraft
} from '$lib/server/govee-store';
import { auditDashboardImpersonation } from '$lib/server/services';
import type { Session } from '$lib/server/session';
import { env } from '$env/dynamic/private';
import { fail, redirect } from '@sveltejs/kit';

function effectiveId(session: Session | null | undefined): string {
  return session?.delegate_of ?? session?.user_id ?? 'demo';
}

// A delegate needs the 'modules' section; a normal login always may. Govee is a
// module, so it rides the same grant as the modules page.
function gate(session: Session | null | undefined): void {
  if (session?.delegate_of && !(session.sections ?? []).includes('modules')) {
    throw redirect(302, '/');
  }
}

function demoView(): GoveeView {
  return {
    enabled: true,
    keyPresent: true,
    binding: {
      device: 'AB:CD:EF:12:34:56',
      sku: 'H6159',
      deviceName: 'Desk strip',
      onRedeem: 'fulfill',
      rewardId: 'demo-reward',
      reward: { rewardId: 'demo-reward', title: 'Colour my lights', cost: 500 },
      allowOffline: false
    }
  };
}

function demoDevices(): GoveeDevice[] {
  return [
    { device: 'AB:CD:EF:12:34:56', sku: 'H6159', name: 'Desk strip', color: true },
    { device: '11:22:33:44:55:66', sku: 'H6072', name: 'Floor lamp', color: true },
    { device: '99:88:77:66:55:44', sku: 'H5081', name: 'Smart plug', color: false }
  ];
}

export const load: PageServerLoad = async ({ locals }) => {
  gate(locals.session);
  const uid = effectiveId(locals.session);
  const colors = [...GOVEE_COLOR_NAMES];

  if (env.DEMO === '1') {
    return { ...demoView(), devices: demoDevices(), colors };
  }

  try {
    const store = goveeStore(uid);
    const view = await store.read();
    // Only list devices once a key is on file; a lookup failure degrades to an
    // empty list plus a flag, never a broken page.
    const { devices, error: deviceError } = view.keyPresent ? await store.listDevices() : { devices: [], error: undefined };
    return { ...view, devices, deviceError, colors };
  } catch {
    return {
      enabled: false,
      keyPresent: false,
      binding: { device: '', sku: '', deviceName: '', onRedeem: 'fulfill' as GoveeOnRedeem, rewardId: '', reward: null, allowOffline: false },
      devices: [] as GoveeDevice[],
      colors,
      degraded: true
    };
  }
};

function requireSession(locals: App.Locals): string | null {
  if (env.DEMO !== '1' && !locals.session) return null;
  return effectiveId(locals.session);
}

// resultFail maps a store failure to a SvelteKit fail(): a missing-scope
// rejection carries a flag so the page shows the reconnect CTA.
function resultFail(r: Extract<GoveeResult, { ok: false }>) {
  if (r.missingScope) return fail(403, { ok: false, missingScope: true });
  return fail(400, { ok: false, error: r.error ?? 'failed' });
}

function asOnRedeem(v: FormDataEntryValue | null): GoveeOnRedeem {
  return v === 'cancel' || v === 'leave' ? v : 'fulfill';
}

// run is the shared action skeleton: gate, resolve the session, short-circuit in
// demo, then run the store operation with uniform error handling + audit. Each
// action only parses its own form and calls run, so there is one copy of the
// gate/try/audit dance instead of one per verb.
async function run(
  locals: App.Locals,
  audit: { action: string; detail: string },
  work: (store: GoveeStore) => Promise<GoveeResult>
) {
  gate(locals.session);
  const uid = requireSession(locals);
  if (uid === null) return fail(401, { ok: false, error: 'Not signed in.' });
  if (env.DEMO === '1') return { ok: true };

  let res: GoveeResult;
  try {
    res = await work(goveeStore(uid));
  } catch (e) {
    console.error(`[govee] ${audit.action} failed:`, e instanceof Error ? (e.stack ?? e.message) : e);
    return fail(400, { ok: false });
  }
  if (!res.ok) return resultFail(res);
  auditDashboardImpersonation(locals.session, audit.action, audit.detail);
  return { ok: true };
}

export const actions: Actions = {
  saveKey: async ({ request, locals }) => {
    const key = String((await request.formData()).get('key') ?? '').trim();
    if (!key) return fail(400, { ok: false, error: 'Enter your Govee API key.' });
    return run(locals, { action: 'govee:key_set', detail: '' }, (s) => s.setKey(key));
  },

  clearKey: ({ locals }) => run(locals, { action: 'govee:key_clear', detail: '' }, (s) => s.clearKey()),

  pickDevice: async ({ request, locals }) => {
    const f = await request.formData();
    const device: GoveeDevice = {
      device: String(f.get('device') ?? '').trim(),
      sku: String(f.get('sku') ?? '').trim(),
      name: String(f.get('deviceName') ?? '').trim(),
      color: true
    };
    if (!device.device || !device.sku) return fail(400, { ok: false, error: 'Pick a device.' });
    return run(locals, { action: 'govee:device', detail: device.name || device.device }, (s) => s.setDevice(device));
  },

  saveReward: async ({ request, locals }) => {
    const f = await request.formData();
    const title = String(f.get('title') ?? '').trim();
    const cost = Math.trunc(Number(f.get('cost')));
    if (!title || title.length > 45) return fail(400, { ok: false, error: 'Title is required (max 45 characters).' });
    if (!Number.isFinite(cost)) return fail(400, { ok: false, error: 'Enter a valid point cost.' });
    if (cost < 1 || cost > 10_000_000) return fail(400, { ok: false, error: 'Enter a valid point cost.' });
    const draft: RewardDraft = { title, cost, onRedeem: asOnRedeem(f.get('onRedeem')) };
    return run(locals, { action: 'govee:reward', detail: title }, (s) => s.saveReward(draft));
  },

  deleteReward: ({ locals }) => run(locals, { action: 'govee:reward_delete', detail: '' }, (s) => s.deleteReward()),

  toggle: async ({ request, locals }) => {
    const enabled = (await request.formData()).get('is_enabled') === 'on';
    return run(locals, { action: 'govee:toggle', detail: String(enabled) }, (s) => s.setEnabled(enabled));
  },

  // liveOnly flips the live-only gate. allow_offline='on' disables it (viewers can
  // drive the lights while offline); the page guards that direction with a
  // warning modal before posting here.
  liveOnly: async ({ request, locals }) => {
    const allowOffline = (await request.formData()).get('allow_offline') === 'on';
    return run(locals, { action: 'govee:live_only', detail: allowOffline ? 'off' : 'on' }, (s) => s.setAllowOffline(allowOffline));
  }
};
