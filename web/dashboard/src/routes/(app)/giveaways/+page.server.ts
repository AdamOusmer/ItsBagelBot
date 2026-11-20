// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail } from '@sveltejs/kit';
import { dev } from '$app/env';
import { GIVEAWAY_LIMITS as pins, MOD, moduleDef, translate } from '@bagel/kit';
import { env } from '@bagel/kit/server/env';
import { RpcError } from '@bagel/kit/server/nats';
import { logger } from '@bagel/kit/server/logger';
import { mutateAction, thrownStatus } from '@bagel/kit/server/form-action';
import { effectiveId } from '#lib/server/board';
import {
  DrawRefusal, emptyGiveawaySettings, giveawayDef, giveawaySettings, mutateGiveaway, pastWinners,
  type DrawMutation
} from '#lib/server/channel-giveaways';
import { emptyLiveRaffles, liveDraws, mutateRaffle, raffleInput } from '#lib/server/channel-raffles';
import { patchModuleSettings } from '#lib/server/module-patch';
import { patchModule } from '#lib/server/commands-store';
import { drawInput, presetPatch, settingPatch } from '#lib/server/giveaway-input';
import { assertModuleWritable, gateModulePage } from '#lib/server/module-gate';
import { auditDashboardImpersonation } from '#lib/server/services';
import type { Actions, PageServerLoad } from './$types';

const DEMO = dev && env.DEMO === '1';
const WINNERS_PER_PAGE = 20;
const raffleDef = moduleDef(MOD.raffle)!;

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
  if (DEMO) return {
    ...emptyGiveawaySettings,
    degraded: false,
    demo: true,
    serverNow: Date.now(),
    live: { rounds: [], degraded: false, raffles: emptyLiveRaffles },
    history: { page: 1, winners: [], more: false, degraded: false }
  };

  const uid = effectiveId(locals.session);
  const [settings, live, history] = await Promise.all([
    giveawaySettings(uid),
    liveDraws(uid),
    winnerHistory(uid, url)
  ]);
  return { ...settings, demo: false, serverNow: Date.now(), live, history };
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
  'draw.open.busy_giveaway': 'busy',
  'draw.open.busy_raffle': 'busyRaffle',
  busy_giveaway: 'busy',
  busy_raffle: 'busyRaffle',
  loyalty_off: 'raffleLoyaltyOff',
  invalid: 'invalidInput'
};

type Action = Actions[string];

function failedDraw(err: unknown, locals: App.Locals) {
  const status = thrownStatus(err instanceof DrawRefusal ? err.refusal : err);
  const fallback = status === 409 ? 'changed' : 'actionFailed';
  const key = err instanceof DrawRefusal && err.reason ? refusalCopy[err.reason] ?? fallback : fallback;
  return fail(status, { ok: false as const, error: translate(locals.locale, `giveaways.${key}`) });
}

type RaffleVerb = Parameters<typeof raffleInput>[0];
type DrawAction = { kind: 'giveaway'; verb: DrawMutation } | { kind: 'raffle'; verb: RaffleVerb };

function drawAction(action: DrawAction): Action {
  const { kind, verb } = action;
  return (event) => {
    const { locals } = event;
    const def = kind === 'raffle' ? raffleDef : giveawayDef;
    return mutateAction(event, {
      gate: () => authorizedSession(locals, def),
      refusal: () => denied(locals),
      demo: DEMO,
      invalid: translate(locals.locale, 'giveaways.invalidInput'),
      run: (session, form) => runDraw(action, { userId: effectiveId(session), actorId: session.user_id }, form),
      failed: (err) => failedDraw(err, locals),
      audited: (actor, detail) => auditDashboardImpersonation(actor, `${kind}:${verb}`, detail)
    });
  };
}

async function runDraw(action: DrawAction, actor: Parameters<typeof mutateRaffle>[0], form: FormData) {
  if (action.kind === 'raffle') return runRaffle(action.verb, actor, form);
  const input = drawInput(action.verb, form);
  if (!input) return null;
  await mutateGiveaway(action.verb, actor, input);
  return input.round_id ?? '';
}

async function runRaffle(verb: RaffleVerb, actor: Parameters<typeof mutateRaffle>[0], form: FormData) {
  const mutation = raffleInput(verb, form);
  if (!mutation) return null;
  const reply = await mutateRaffle(actor, mutation);
  return mutation.verb === 'start' ? reply.round_id ?? '' : mutation.input.round_id;
}

function authorizedSession(locals: App.Locals, def = giveawayDef) {
  const session = locals.session;
  return session && assertModuleWritable(session, def) ? session : null;
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

function settingsInput(kind: SettingsChange, form: FormData) {
  const partial = settingsPatch(kind, form);
  const expectedRev = Number(form.get('expected_rev') ?? NaN);
  if (!partial) return null;
  if (!Number.isSafeInteger(expectedRev) || expectedRev < 0) return null;
  const enabled = form.get('is_enabled');
  if (enabled !== 'on' && enabled !== 'off') return null;
  return { partial, expectedRev, isEnabled: enabled === 'on' };
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
        const input = settingsInput(kind, form);
        if (!input) return null;
        const result = await patchModule({ userId: effectiveId(session), name: MOD.giveaway, ...input });
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

function giveawayAction(verb: DrawMutation) {
  return drawAction({ kind: 'giveaway', verb });
}

export const actions: Actions = {
  raffleStart: drawAction({ kind: 'raffle', verb: 'start' }),
  raffleDraw: drawAction({ kind: 'raffle', verb: 'draw' }),
  raffleCancel: drawAction({ kind: 'raffle', verb: 'cancel' }),
  raffleSettings: async ({ locals, request }) => {
    if (!authorizedSession(locals, raffleDef)) return denied(locals);
    const { result } = await patchModuleSettings(raffleDef, await request.formData(), locals, DEMO);
    return result;
  },
  start: giveawayAction('start'),
  close: giveawayAction('close'),
  extend: giveawayAction('extend'),
  cancel: giveawayAction('cancel'),
  reroll: giveawayAction('reroll'),
  end: giveawayAction('end_slots'),
  skip: giveawayAction('skip_rule'),
  deliver: giveawayAction('mark_delivered'),
  savePresets: save('presets'),
  saveSettings: save('settings'),
  toggle: save('toggle')
};
