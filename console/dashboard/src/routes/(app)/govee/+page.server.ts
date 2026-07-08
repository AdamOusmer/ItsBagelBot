import type { Actions, PageServerLoad } from './$types';
import { GOVEE_COLOR_NAMES } from '@bagel/shared';
import {
  readGovee,
  listGoveeDevices,
  setGoveeKey,
  clearGoveeKey,
  setGoveeDevice,
  setGoveeEnabled,
  setGoveeAllowOffline,
  saveGoveeReward,
  deleteGoveeReward,
  type GoveeDevice,
  type GoveeOnRedeem,
  type GoveeResult,
  type GoveeView
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
    const view = await readGovee(uid);
    // Only list devices once a key is on file; a lookup failure degrades to an
    // empty list plus a flag, never a broken page.
    let devices: GoveeDevice[] = [];
    let deviceError: string | undefined;
    if (view.keyPresent) {
      const d = await listGoveeDevices(uid);
      devices = d.devices;
      deviceError = d.error;
    }
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

export const actions: Actions = {
  saveKey: async ({ request, locals }) => {
    gate(locals.session);
    const uid = requireSession(locals);
    if (uid === null) return fail(401, { ok: false, error: 'Not signed in.' });

    const key = String((await request.formData()).get('key') ?? '').trim();
    if (!key) return fail(400, { ok: false, error: 'Enter your Govee API key.' });
    if (env.DEMO === '1') return { ok: true };

    let res: GoveeResult;
    try {
      res = await setGoveeKey(uid, key);
    } catch (e) {
      console.error('[govee] saveKey failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false, error: 'Could not save your key.' });
    }
    if (!res.ok) return resultFail(res);
    auditDashboardImpersonation(locals.session, 'govee:key_set', '');
    return { ok: true };
  },

  clearKey: async ({ locals }) => {
    gate(locals.session);
    const uid = requireSession(locals);
    if (uid === null) return fail(401, { ok: false, error: 'Not signed in.' });
    if (env.DEMO === '1') return { ok: true };

    try {
      const res = await clearGoveeKey(uid);
      if (!res.ok) return resultFail(res);
    } catch (e) {
      console.error('[govee] clearKey failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false });
    }
    auditDashboardImpersonation(locals.session, 'govee:key_clear', '');
    return { ok: true };
  },

  pickDevice: async ({ request, locals }) => {
    gate(locals.session);
    const uid = requireSession(locals);
    if (uid === null) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const device = String(f.get('device') ?? '').trim();
    const sku = String(f.get('sku') ?? '').trim();
    const deviceName = String(f.get('deviceName') ?? '').trim();
    if (!device || !sku) return fail(400, { ok: false, error: 'Pick a device.' });
    if (env.DEMO === '1') return { ok: true };

    try {
      const res = await setGoveeDevice(uid, device, sku, deviceName);
      if (!res.ok) return resultFail(res);
    } catch (e) {
      console.error('[govee] pickDevice failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false });
    }
    auditDashboardImpersonation(locals.session, 'govee:device', deviceName || device);
    return { ok: true };
  },

  saveReward: async ({ request, locals }) => {
    gate(locals.session);
    const uid = requireSession(locals);
    if (uid === null) return fail(401, { ok: false, error: 'Not signed in.' });

    const f = await request.formData();
    const title = String(f.get('title') ?? '').trim();
    const cost = Math.trunc(Number(f.get('cost')));
    const onRedeem = String(f.get('onRedeem') ?? 'fulfill') as GoveeOnRedeem;
    if (!title || title.length > 45) return fail(400, { ok: false, error: 'Title is required (max 45 characters).' });
    if (!Number.isFinite(cost) || cost < 1 || cost > 10_000_000) return fail(400, { ok: false, error: 'Enter a valid point cost.' });
    if (env.DEMO === '1') return { ok: true };

    let res: GoveeResult;
    try {
      res = await saveGoveeReward(uid, title, cost, onRedeem);
    } catch (e) {
      console.error('[govee] saveReward failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false, error: 'Could not save the reward.' });
    }
    if (!res.ok) return resultFail(res);
    auditDashboardImpersonation(locals.session, 'govee:reward', title);
    return { ok: true };
  },

  deleteReward: async ({ locals }) => {
    gate(locals.session);
    const uid = requireSession(locals);
    if (uid === null) return fail(401, { ok: false, error: 'Not signed in.' });
    if (env.DEMO === '1') return { ok: true };

    try {
      const res = await deleteGoveeReward(uid);
      if (!res.ok) return resultFail(res);
    } catch (e) {
      console.error('[govee] deleteReward failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false });
    }
    auditDashboardImpersonation(locals.session, 'govee:reward_delete', '');
    return { ok: true };
  },

  toggle: async ({ request, locals }) => {
    gate(locals.session);
    const uid = requireSession(locals);
    if (uid === null) return fail(401, { ok: false, error: 'Not signed in.' });

    const enabled = (await request.formData()).get('is_enabled') === 'on';
    if (env.DEMO === '1') return { ok: true, enabled };

    try {
      const res = await setGoveeEnabled(uid, enabled);
      if (!res.ok) return resultFail(res);
    } catch (e) {
      console.error('[govee] toggle failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false });
    }
    auditDashboardImpersonation(locals.session, 'govee:toggle', String(enabled));
    return { ok: true, enabled };
  },

  // liveOnly flips the live-only gate. allow_offline='on' disables it (viewers can
  // drive the lights while offline); the page guards that direction with a
  // warning modal before posting here.
  liveOnly: async ({ request, locals }) => {
    gate(locals.session);
    const uid = requireSession(locals);
    if (uid === null) return fail(401, { ok: false, error: 'Not signed in.' });

    const allowOffline = (await request.formData()).get('allow_offline') === 'on';
    if (env.DEMO === '1') return { ok: true, allowOffline };

    try {
      const res = await setGoveeAllowOffline(uid, allowOffline);
      if (!res.ok) return resultFail(res);
    } catch (e) {
      console.error('[govee] liveOnly failed:', e instanceof Error ? (e.stack ?? e.message) : e);
      return fail(400, { ok: false });
    }
    auditDashboardImpersonation(locals.session, 'govee:live_only', allowOffline ? 'off' : 'on');
    return { ok: true, allowOffline };
  }
};
