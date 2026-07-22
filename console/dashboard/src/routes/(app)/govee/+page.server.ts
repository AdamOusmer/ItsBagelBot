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
import { logger } from '@bagel/shared/server/logger';
import { gateModulePage } from '$lib/server/module-gate';
import type { Session } from '$lib/server/session';
import { env } from '$env/dynamic/private';
import { fail } from '@sveltejs/kit';

function effectiveId(session: Session | null | undefined): string {
  return session?.delegate_of ?? session?.user_id ?? 'demo';
}

// Delegate scope comes from the govee catalog def (see module-gate.ts).
function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'govee');
}

function demoView(): GoveeView {
  return {
    enabled: true,
    keyPresent: true,
    // One light configured, one left open, so the deck shows both states.
    bindings: [
      {
        device: 'AB:CD:EF:12:34:56',
        sku: 'H6159',
        deviceName: 'Desk strip',
        onRedeem: 'fulfill',
        rewardId: 'demo-reward',
        reward: { rewardId: 'demo-reward', title: 'Colour the desk strip', cost: 500, color: '#9147ff', cooldown: 0 },
        allowOffline: false,
        allowOff: true,
        replyMessage: '@{user} set the lights to {color}!'
      }
    ]
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
      bindings: [] as GoveeView['bindings'],
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
      replyMessage,
      allowOff: f.get('allow_off') === 'on',
      allowOffline: f.get('allow_offline') === 'on'
    }
  };
}

// parseRewardForm resolves the target light and its reward draft, or an error.
// Kept out of the action so the action stays a thin parse-then-run.
function parseRewardForm(f: FormData): { device: GoveeDevice; draft: RewardDraft } | { error: string } {
  const device = String(f.get('device') ?? '').trim();
  const sku = String(f.get('sku') ?? '').trim();
  if (!device || !sku) return { error: 'Pick a light first.' };

  const parsed = parseRewardDraft(f);
  if ('error' in parsed) return parsed;
  return { device: { device, sku, name: String(f.get('deviceName') ?? '').trim(), color: true }, draft: parsed.draft };
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
    logger.error({ err: e }, `[govee] ${audit.action} failed`);
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

  saveReward: async ({ request, locals }) => {
    const parsed = parseRewardForm(await request.formData());
    if ('error' in parsed) return fail(400, { ok: false, error: parsed.error });
    return run(locals, { action: 'govee:reward', detail: parsed.draft.title }, (s) => s.saveReward(parsed.device, parsed.draft));
  },

  deleteReward: async ({ request, locals }) => {
    const deviceId = String((await request.formData()).get('device') ?? '').trim();
    if (!deviceId) return fail(400, { ok: false, error: 'Pick a light first.' });
    return run(locals, { action: 'govee:reward_delete', detail: deviceId }, (s) => s.deleteReward(deviceId));
  },

  toggle: async ({ request, locals }) => {
    const enabled = (await request.formData()).get('is_enabled') === 'on';
    return run(locals, { action: 'govee:toggle', detail: String(enabled) }, (s) => s.setEnabled(enabled));
  }
};
