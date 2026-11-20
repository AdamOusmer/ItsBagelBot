// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail } from '@sveltejs/kit';
import { dev } from '$app/env';
import { GIVEAWAY_LIMITS as pins, MOD, translate } from '@bagel/kit';
import { env } from '@bagel/kit/server/env';
import { RpcError } from '@bagel/kit/server/nats';
import { logger } from '@bagel/kit/server/logger';
import { mutateAction, thrownStatus } from '@bagel/kit/server/form-action';
import type { GiveawayRound } from '#lib/channel-giveaway';
import { effectiveId } from '#lib/server/board';
import {
  DrawRefusal, emptyGiveawaySettings, giveawayDef, giveawaySettings, liveGiveaways, mutateGiveaway, pastWinners,
  type DrawActor, type DrawInput, type DrawMutation
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
  if (DEMO) return {
    ...emptyGiveawaySettings,
    degraded: false,
    demo: true,
    serverNow: Date.now(),
    live: { rounds: [], degraded: false },
    history: { page: 1, winners: [], more: false, degraded: false }
  };

  const uid = effectiveId(locals.session);
  const [settings, live, history] = await Promise.all([
    giveawaySettings(uid),
    liveGiveaways(uid),
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
  'giveaway.open.busy': 'busyRaffle',
  'giveaway.extend.snapshot_expiry': 'snapshotExpiry',
  'draw.open.busy_giveaway': 'busy',
  'draw.open.busy_raffle': 'busyRaffle'
};

const slotConflictCopy: Partial<Record<DrawMutation, string>> = {
  reroll: 'rerollUnavailable',
  end_slots: 'endSlotsUnavailable',
  skip_rule: 'skipUnavailable'
};

const slotPhaseCopy: Partial<Record<GiveawayRound['phase'], string>> = {
  open: 'slotsBeforeDraw',
  closing: 'slotsDrawing'
};

type Action = Actions[string];

function failedDraw(err: unknown, locals: App.Locals, conflictKey = 'changed') {
  const status = thrownStatus(err instanceof DrawRefusal ? err.refusal : err);
  const fallback = status === 409 ? conflictKey : 'actionFailed';
  const key = err instanceof DrawRefusal && err.reason ? refusalCopy[err.reason] ?? fallback : fallback;
  return fail(status, { ok: false as const, error: translate(locals.locale, `giveaways.${key}`) });
}

function unresolvedSlotConflict(err: unknown): boolean {
  if (!(err instanceof DrawRefusal) || err.reason) return false;
  return err.refusal.code === 'conflict' && err.refusal.message === 'giveaway action no longer applies';
}

async function slotRefusalCopy(err: unknown, verb: DrawMutation, actor: DrawActor, input: DrawInput) {
  const fallback = slotConflictCopy[verb];
  if (!fallback || !unresolvedSlotConflict(err)) return fallback;
  const live = await liveGiveaways(actor.userId);
  if (live.degraded) return fallback;
  const round = live.rounds.find(({ id }) => id === input.round_id);
  return round ? slotPhaseCopy[round.phase] ?? fallback : 'slotsRoundEnded';
}

function giveawayAction(verb: DrawMutation): Action {
  return (event) => {
    const { locals } = event;
    return mutateAction(event, {
      gate: async () => DEMO ? (await import('#lib/server/demo-data')).demoSession() : authorizedSession(locals),
      refusal: () => denied(locals),
      demo: DEMO,
      invalid: translate(locals.locale, 'giveaways.invalidInput'),
      run: (session, form) => runDraw(verb, { userId: effectiveId(session), actorId: session.user_id }, form, locals),
      failed: (err) => failedDraw(err, locals),
      audited: (actor, detail) => auditDashboardImpersonation(actor, `giveaway:${verb}`, detail)
    });
  };
}

async function runDraw(verb: DrawMutation, actor: DrawActor, form: FormData, locals: App.Locals) {
  const input = drawInput(verb, form);
  if (!input) return null;
  try {
    await mutateGiveaway(verb, actor, input);
    return input.round_id ?? '';
  } catch (err) {
    return failedDraw(err, locals, await slotRefusalCopy(err, verb, actor, input));
  }
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

export const actions: Actions = {
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
