// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail } from '@sveltejs/kit';
import { dev } from '$app/env';
import { GIVEAWAY_LIMITS as pins, MOD, translate } from '@bagel/kit';
import { env } from '@bagel/kit/server/env';
import { RpcError } from '@bagel/kit/server/nats';
import { logger } from '@bagel/kit/server/logger';
import { mutateAction, thrownStatus } from '@bagel/kit/server/form-action';
import { effectiveId } from '#lib/server/board';
import {
  GiveawayRefusal, emptyGiveawaySettings, giveawayDef, giveawaySettings, liveGiveaways, mutateGiveaway, pastWinners,
  type DrawMutation
} from '#lib/server/channel-giveaways';
import { patchModule } from '#lib/server/commands-store';
import { drawInput, presetPatch, settingPatch } from '#lib/server/giveaway-input';
import { assertModuleWritable, gateModulePage } from '#lib/server/module-gate';
import { auditDashboardImpersonation } from '#lib/server/services';
import type { Actions, PageServerLoad } from './$types';

const DEMO = dev && env.DEMO === '1';
const WINNERS_PER_PAGE = 20;

async function winnerHistory(userId: string, url: URL) {
  const selected = Number(url.searchParams.get('winners'));
  const page = Number.isSafeInteger(selected * WINNERS_PER_PAGE) && Number.isSafeInteger(selected) ? Math.max(1, selected) : 1;
  try {
    const winners = await pastWinners(userId, Math.min(WINNERS_PER_PAGE + 1, pins.giveawayMaxRecentPage), (page - 1) * WINNERS_PER_PAGE);
    return { page, winners: winners.slice(0, WINNERS_PER_PAGE), more: winners.length > WINNERS_PER_PAGE, degraded: false };
  } catch {
    return { page, winners: [], more: false, degraded: true };
  }
}

export const load: PageServerLoad = async ({ locals, depends, url }) => {
  gateModulePage(locals.session, MOD.giveaway);
  depends('giveaways:live');
  const serverNow = Date.now();
  if (DEMO) return {
    ...emptyGiveawaySettings,
    degraded: false,
    demo: true,
    serverNow,
    live: { rounds: [], degraded: false },
    history: { page: 1, winners: [], more: false, degraded: false }
  };

  const uid = effectiveId(locals.session);
  const [settings, live, history] = await Promise.all([
    giveawaySettings(uid),
    liveGiveaways(uid),
    winnerHistory(uid, url)
  ]);
  return { ...settings, demo: false, serverNow, live, history };
};

const refusalCopy: Record<string, string> = {
  'giveaway.open.disabled': 'disabled',
  'giveaway.open.preset': 'invalidPreset',
  'giveaway.open.prize': 'prizeRequired',
  'giveaway.open.prize_length': 'prizeLength',
  'giveaway.prize_blocked': 'prizeBlocked',
  'giveaway.open.winners': 'invalidInput',
  'giveaway.open.window': 'invalidInput',
  'giveaway.open.loyalty_off': 'loyaltyOff',
  'giveaway.open.stalled': 'loyaltyStalled',
  'giveaway.open.busy': 'busy',
  'draw.open.busy_giveaway': 'busy'
};

type Action = Actions[string];

function failedDraw(err: unknown, locals: App.Locals) {
  const status = thrownStatus(err instanceof GiveawayRefusal ? err.refusal : err);
  const fallback = status === 409 ? 'changed' : 'actionFailed';
  const key = err instanceof GiveawayRefusal && err.reason ? refusalCopy[err.reason] ?? fallback : fallback;
  return fail(status, { ok: false as const, error: translate(locals.locale, `giveaways.${key}`) });
}

function drawAction(verb: DrawMutation): Action {
  return (event) => {
    const { locals } = event;
    const session = authorizedSession(locals);
    return mutateAction(event, {
      gate: () => session,
      refusal: () => denied(locals),
      demo: DEMO,
      invalid: translate(locals.locale, 'giveaways.invalidInput'),
      run: async (actor, form) => {
        const input = drawInput(verb, form);
        if (!input) return null;
        await mutateGiveaway(verb, { userId: effectiveId(actor), actorId: actor.user_id }, input);
        return input.round_id ?? '';
      },
      failed: (err) => failedDraw(err, locals),
      audited: (actor, detail) => auditDashboardImpersonation(actor, `giveaway:${verb}`, detail)
    });
  };
}

function authorizedSession(locals: App.Locals) {
  const session = locals.session;
  return session && assertModuleWritable(session, giveawayDef) ? session : null;
}

function denied(locals: App.Locals) {
  const key = locals.session ? 'serverErrors.notAllowed' : 'serverErrors.notSignedIn';
  return fail(locals.session ? 403 : 401, { ok: false as const, error: translate(locals.locale, key) });
}

type SettingsChange = 'settings' | 'presets' | 'toggle';

function settingsPatch(kind: SettingsChange, form: FormData) {
  switch (kind) {
    case 'settings': return settingPatch(form);
    case 'presets': return presetPatch(form);
    case 'toggle': return {};
  }
}

function save(kind: SettingsChange): Action {
  return (event) => {
    const { locals } = event;
    return mutateAction(event, {
      gate: () => authorizedSession(locals),
      refusal: () => denied(locals),
      demo: DEMO,
      invalid: translate(locals.locale, kind === 'presets' ? 'giveaways.invalidPresets' : 'giveaways.invalidInput'),
      run: async (session, form) => {
        const partial = settingsPatch(kind, form);
        const expectedRev = Number(form.get('expected_rev'));
        const enabled = form.get('is_enabled');
        if (!partial || !form.has('expected_rev')) return null;
        if (!Number.isSafeInteger(expectedRev) || expectedRev < 0) return null;
        if (enabled !== 'on' && enabled !== 'off') return null;
        const result = await patchModule({ userId: effectiveId(session), name: MOD.giveaway, isEnabled: enabled === 'on', partial, expectedRev });
        if (result.conflict) return failedDraw(new RpcError('module revision changed', 'conflict'), locals);
        return kind;
      },
      failed: (err) => {
        logger.warn({ err }, '[giveaways] settings update failed');
        return failedDraw(err, locals);
      },
      audited: (session, detail) => auditDashboardImpersonation(session, 'giveaway:settings', detail)
    });
  };
}

export const actions: Actions = {
  start: drawAction('start'),
  close: drawAction('close'),
  extend: drawAction('extend'),
  cancel: drawAction('cancel'),
  reroll: drawAction('reroll'),
  end: drawAction('end_slots'),
  skip: drawAction('skip_rule'),
  deliver: drawAction('mark_delivered'),
  savePresets: save('presets'),
  saveSettings: save('settings'),
  toggle: save('toggle')
};
