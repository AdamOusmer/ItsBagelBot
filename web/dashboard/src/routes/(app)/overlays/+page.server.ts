// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail, redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { translate, type Locale } from '@bagel/kit/i18n';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { RpcError } from '@bagel/kit/server/nats';
import {
  MAX_EVERY_MINUTES, MAX_SHOW_SECONDS, MIN_EVERY_MINUTES, MIN_SHOW_SECONDS, type PoweredConfigError
} from '@bagel/kit/overlays';
import { hasOwnerAccess } from '#lib/server/owner';
import { auditDashboardImpersonation } from '#lib/server/services/audit';
import { poweredGet, poweredRotate, poweredSave } from '#lib/server/services/overlays';
import { parsePoweredSave } from './powered-form';
import type { Session } from '#lib/server/session';
import type { Actions, PageServerLoad } from './$types';

const DEMO = dev && env.DEMO === '1';

const LIMITS = { everyMin: MIN_EVERY_MINUTES, everyMax: MAX_EVERY_MINUTES, showMin: MIN_SHOW_SECONDS, showMax: MAX_SHOW_SECONDS };

type ParsedSave = Extract<ReturnType<typeof parsePoweredSave>, { error: null }>;

function invalidSave(locale: Locale, error: PoweredConfigError) {
  return fail(400, { error: translate(locale, error, LIMITS) });
}

function saveRefused(err: unknown, locale: Locale) {
  const code = err instanceof RpcError ? err.code : '';
  if (code === 'conflict') return fail(409, { conflict: true });
  if (code === 'invalid') return fail(400, { error: translate(locale, 'overlays.saveInvalid') });
  return fail(503, { error: translate(locale, 'overlays.saveFailed') });
}

async function persistPowered(session: Session, save: ParsedSave, locale: Locale) {
  try {
    const view = await poweredSave(session, { config: save.config, revision: save.revision });
    auditDashboardImpersonation(session, 'overlays:powered_save');
    return { ok: true, rev: view.revision };
  } catch (err) {
    return saveRefused(err, locale);
  }
}

function ownerRefusal(session: Session | null, locale: Locale, key: 'error' | 'rotateError') {
  if (!session) return fail(401, { [key]: translate(locale, 'serverErrors.notSignedIn') });
  if (!hasOwnerAccess(session)) return fail(403, { [key]: translate(locale, 'serverErrors.ownerOnly') });
  return null;
}

async function demoSave(fields: FormData, locale: Locale) {
  const save = parsePoweredSave(fields);
  if (save.error !== null) return invalidSave(locale, save.error);
  return { ok: true, rev: (await import('#lib/server/demo-data')).demoPoweredSave(save.config) };
}

const demoActions = {
  save: async ({ locals: { locale }, request }) => demoSave(await request.formData(), locale),
  rotate: async () => (await import('#lib/server/demo-data')).demoPoweredRotate()
} satisfies Actions;

const demoLoad = (async () => ({
  readOnly: false,
  powered: (await import('#lib/server/demo-data')).demoPoweredView()
})) satisfies PageServerLoad;

const liveLoad = (async ({ locals: { session } }) => {
  if (!session) throw redirect(302, '/login?next=%2Foverlays');
  if (!hasOwnerAccess(session)) return { readOnly: true, powered: null };
  return { readOnly: false, powered: await bestEffort(poweredGet(session), null) };
}) satisfies PageServerLoad;

export const load: PageServerLoad = DEMO ? demoLoad : liveLoad;

const liveActions = {
  save: async ({ locals: { session, locale }, request }) => {
    const refused = ownerRefusal(session, locale, 'error');
    if (refused || !session) return refused;

    const save = parsePoweredSave(await request.formData());
    if (save.error !== null) return invalidSave(locale, save.error);
    return persistPowered(session, save, locale);
  },
  rotate: async ({ locals: { session, locale }, setHeaders }) => {
    const refused = ownerRefusal(session, locale, 'rotateError');
    if (refused || !session) return refused;

    setHeaders({ 'cache-control': 'no-store' });
    try {
      const { url, revision } = await poweredRotate(session);
      auditDashboardImpersonation(session, 'overlays:powered_rotate');
      return { url, revision };
    } catch {
      return fail(503, { rotateError: translate(locale, 'overlays.replaceFailed') });
    }
  }
} satisfies Actions;

export const actions = DEMO ? demoActions : liveActions;
