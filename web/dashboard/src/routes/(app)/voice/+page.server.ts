// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail, redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { translate, type Locale } from '@bagel/kit/i18n';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { RpcError } from '@bagel/kit/server/nats';
import { MAX_CLIP_SECONDS, MIN_CLIP_SECONDS } from '@bagel/kit/voice-plan';
import { listModulesForEditing, patchModule, type ModuleView } from '#lib/server/commands-store';
import { moduleEditState } from '#lib/server/module-edit-state';
import { hasOwnerAccess } from '#lib/server/owner';
import { auditDashboardImpersonation } from '#lib/server/services/audit';
import { voiceGet, voiceOverlayGet, voiceOverlayRotate, voiceSet, voiceTestAlert } from '#lib/server/services/voice';
import { TTS_COMMAND } from '@bagel/kit/voice-plan';
import { parseCommandSave, parseVoiceSave, ttsCommandConfig, voiceAlertConfig } from './voice-settings';
import type { Session } from '#lib/server/session';
import type { Actions, PageServerLoad } from './$types';

const DEMO = dev && env.DEMO === '1';

async function readRow(userId: string, module: string) {
  const rows = await listModulesForEditing(userId);
  return rows.find(({ name }) => name === module);
}

const readAlerts = (userId: string) => readRow(userId, 'alerts');

function commandView(row: ModuleView | undefined) {
  const { config, revision } = moduleEditState(row);
  return { config: ttsCommandConfig(config), enabled: row?.is_enabled ?? false, revision };
}

function alertsView(row: ModuleView | undefined) {
  const { config, revision } = moduleEditState(row);
  return { config: voiceAlertConfig(config), revision };
}

async function currentRevision(userId: string) {
  return moduleEditState(await readAlerts(userId)).revision;
}

async function saveAlerts(userId: string, save: { partial: Record<string, string>; expectedRev: number | null }) {
  if (Object.keys(save.partial).length === 0) return { conflict: false, rev: save.expectedRev ?? await currentRevision(userId) };
  const alerts = await readAlerts(userId);
  return patchModule({
    userId,
    name: 'alerts',
    isEnabled: alerts?.is_enabled ?? true,
    partial: save.partial,
    expectedRev: save.expectedRev ?? 0
  });
}

function invalidSave(locale: Locale, error: string) {
  return fail(400, { error: translate(locale, error, { min: MIN_CLIP_SECONDS, max: MAX_CLIP_SECONDS }) });
}

type ParsedSave = Extract<ReturnType<typeof parseVoiceSave>, { error: null }>;
type ParsedCommand = Extract<ReturnType<typeof parseCommandSave>, { error: null }>;

async function persistCommand(session: Session, save: ParsedCommand, locale: Locale) {
  try {
    const allowance = await voiceGet(session);
    if (allowance.reason !== 'ok') return fail(403, { error: translate(locale, 'voice.saveUnavailable') });
    const current = commandView(await readRow(session.user_id, TTS_COMMAND));
    const result = await patchModule({
      userId: session.user_id, name: TTS_COMMAND, isEnabled: save.enabled ?? current.enabled,
      partial: save.partial, expectedRev: save.expectedRev
    });
    if (result.conflict) return fail(409, { conflict: true });
    auditDashboardImpersonation(session, 'voice:command_save');
    return { ok: true, rev: result.rev };
  } catch {
    return fail(503, { error: translate(locale, 'voice.saveFailed') });
  }
}

async function persistVoice(session: Session, save: ParsedSave, locale: Locale) {
  let revision = save.expectedRev ?? undefined;
  try {
    const allowance = await voiceGet(session);
    if (allowance.reason !== 'ok') return fail(403, { error: translate(locale, 'voice.saveUnavailable') });

    const alerts = await saveAlerts(session.user_id, save);
    if (alerts.conflict) return fail(409, { conflict: true });
    revision = alerts.rev;
    if (Object.keys(save.settings).length > 0) {
      const { settings, revision } = await voiceOverlayGet(session);
      await voiceSet(session, { settings: { ...settings, ...save.settings }, revision });
    }
    auditDashboardImpersonation(session, 'voice:save');
    return { ok: true, rev: alerts.rev };
  } catch (err) {
    if (err instanceof RpcError && err.code === 'conflict') return fail(409, { conflict: true });
    return fail(503, { error: translate(locale, 'voice.saveFailed'), revision });
  }
}

async function demoSave(fields: FormData, locale: Locale) {
  const save = parseVoiceSave(fields);
  if (save.error !== null) return invalidSave(locale, save.error);
  return { ok: true, rev: (await import('#lib/server/demo-data')).demoVoiceSave(save.settings, save.partial) };
}

const demoActions = {
  save: async ({ locals: { locale }, request }) => demoSave(await request.formData(), locale),
  saveCommand: async ({ locals: { locale }, request }) => {
    const save = parseCommandSave(await request.formData());
    if (save.error !== null) return invalidSave(locale, save.error);
    return { ok: true, rev: save.expectedRev + 1 };
  },
  rotateOverlay: async () => ({ overlayUrl: 'https://overlay.itsbagelbot.com/tts#demo' }),
  testAlert: async () => ({ testAlertSent: true })
} satisfies Actions;

const demoLoad = (async ({ url }) => {
  const { allowance, overlay, config, revision } = (await import('#lib/server/demo-data')).demoVoiceView(url.searchParams.get('locked') === '1');
  const locked = allowance.reason === 'no_plan';
  return {
    readOnly: false,
    locked,
    allowance,
    overlay,
    alerts: locked ? null : { config: voiceAlertConfig(config), revision },
    command: locked ? null : { config: ttsCommandConfig({}), enabled: false, revision: 0 }
  };
}) satisfies PageServerLoad;

const liveLoad = (async ({ locals: { session } }) => {
  if (!session) throw redirect(302, '/login?next=%2Fvoice');
  if (!hasOwnerAccess(session)) return { readOnly: true, locked: false, allowance: null, overlay: null, alerts: null, command: null };

  const allowance = await bestEffort(voiceGet(session), null);
  const view = { readOnly: false, locked: allowance?.reason === 'no_plan', allowance };
  if (allowance?.reason !== 'ok') return { ...view, overlay: null, alerts: null, command: null };

  const [overlay, rows] = await Promise.all([
    bestEffort(voiceOverlayGet(session), null),
    bestEffort(listModulesForEditing(session.user_id), null)
  ]);
  if (!rows || !overlay) return { ...view, overlay, alerts: null, command: null };
  const byName = (module: string) => rows.find(({ name }) => name === module);
  return { ...view, overlay, alerts: alertsView(byName('alerts')), command: commandView(byName(TTS_COMMAND)) };
}) satisfies PageServerLoad;

export const load: PageServerLoad = DEMO ? demoLoad : liveLoad;

const testAlertRefusals = {
  no_link: [409, 'voice.testNoLink'],
  no_minutes: [409, 'voice.testNoMinutes'],
  rate_limited: [429, 'voice.testRateLimited'],
  disabled: [403, 'voice.testDisabled']
} as const;

const liveActions = {
  save: async ({ locals: { session, locale }, request }) => {
    if (!session) return fail(401, { error: translate(locale, 'serverErrors.notSignedIn') });
    if (!hasOwnerAccess(session)) return fail(403, { error: translate(locale, 'serverErrors.ownerOnly') });

    const save = parseVoiceSave(await request.formData());
    if (save.error !== null) return invalidSave(locale, save.error);
    return persistVoice(session, save, locale);
  },
  saveCommand: async ({ locals: { session, locale }, request }) => {
    if (!session) return fail(401, { error: translate(locale, 'serverErrors.notSignedIn') });
    if (!hasOwnerAccess(session)) return fail(403, { error: translate(locale, 'serverErrors.ownerOnly') });

    const save = parseCommandSave(await request.formData());
    if (save.error !== null) return invalidSave(locale, save.error);
    return persistCommand(session, save, locale);
  },
  rotateOverlay: async ({ locals: { session, locale }, setHeaders }) => {
    if (!session) return fail(401, { overlayError: translate(locale, 'serverErrors.notSignedIn') });
    if (!hasOwnerAccess(session)) return fail(403, { overlayError: translate(locale, 'serverErrors.ownerOnly') });

    setHeaders({ 'cache-control': 'no-store' });
    try {
      const overlayUrl = await voiceOverlayRotate(session);
      auditDashboardImpersonation(session, 'voice:overlay_rotate');
      return { overlayUrl };
    } catch (err) {
      if (err instanceof RpcError && err.code === 'forbidden') {
        return fail(403, { overlayError: translate(locale, 'voice.overlayUnavailable') });
      }
      return fail(503, { overlayError: translate(locale, 'voice.overlayFailed') });
    }
  },
  testAlert: async ({ locals: { session, locale } }) => {
    if (!session) return fail(401, { testAlertError: translate(locale, 'serverErrors.notSignedIn') });
    if (!hasOwnerAccess(session)) return fail(403, { testAlertError: translate(locale, 'serverErrors.ownerOnly') });

    try {
      const status = await voiceTestAlert(session);
      if (status === 'queued') {
        auditDashboardImpersonation(session, 'voice:test_alert');
        return { testAlertSent: true };
      }
      const [httpStatus, message] = testAlertRefusals[status];
      return fail(httpStatus, { testAlertError: translate(locale, message) });
    } catch {
      return fail(503, { testAlertError: translate(locale, 'voice.testFailed') });
    }
  }
} satisfies Actions;

export const actions = DEMO ? demoActions : liveActions;
