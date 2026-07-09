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
      reward: { rewardId: 'demo-reward', title: 'Colour my lights', cost: 500, color: '#9147ff', cooldown: 0 },
      allowOffline: false,
      allowOff: true,
      replyMessage: '@{user} set the lights to {color}!'
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
    return { ...demoView(), devices: { devices: demoDevices(), error: undefined }, colors };
  }

  try {
    const store = goveeStore(uid);
    const view = await store.read();
    // The device list is the one slow read (a Govee cloud round trip). Stream it
    // as an unresolved promise so SSR emits the shell + the key/reward steps
    // instantly and the picker fills in when it lands. Only fetch once a key is
    // on file; a lookup failure degrades to an empty list plus a flag on the
    // resolved value, never a broken page.
    const devices = view.keyPresent
      ? store.listDevices()
      : Promise.resolve({ devices: [] as GoveeDevice[], error: undefined });
    return { ...view, devices, colors };
  } catch {
    return {
      enabled: false,
      keyPresent: false,
      binding: { device: '', sku: '', deviceName: '', onRedeem: 'fulfill' as GoveeOnRedeem, rewardId: '', reward: null, allowOffline: false, allowOff: false, replyMessage: '' },
      devices: { devices: [] as GoveeDevice[], error: undefined },
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

// parseRewardForm validates the reward editor's fields and returns the draft, or
// a user-facing error message. Kept out of the action so the action stays a thin
// parse-then-run.
function parseRewardForm(f: FormData): { draft: RewardDraft } | { error: string } {
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
    draft: { title, cost, onRedeem: asOnRedeem(f.get('onRedeem')), color, cooldown, replyMessage, allowOff: f.get('allow_off') === 'on' }
  };
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
    const parsed = parseRewardForm(await request.formData());
    if ('error' in parsed) return fail(400, { ok: false, error: parsed.error });
    return run(locals, { action: 'govee:reward', detail: parsed.draft.title }, (s) => s.saveReward(parsed.draft));
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
