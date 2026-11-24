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
import {
  overlayAddTemplate, overlayDelete, overlayGet, overlayList, overlayRotate, overlaySave
} from '#lib/server/services/overlays';
import { POWERED_ORIGIN } from '@bagel/kit/overlays';
import { parsePoweredSave } from './powered-form';
import type { OverlayView } from '#lib/server/services/overlays';
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
    const view = await overlaySave(session, { id: save.id, config: save.config, revision: save.revision });
    auditDashboardImpersonation(session, 'overlays:powered_save');
    return { ok: true, rev: view.revision };
  } catch (err) {
    return saveRefused(err, locale);
  }
}

function ownerRefusal(session: Session | null, locale: Locale, key: 'error' | 'listError') {
  if (!session) return fail(401, { [key]: translate(locale, 'serverErrors.notSignedIn') });
  if (!hasOwnerAccess(session)) return fail(403, { [key]: translate(locale, 'serverErrors.ownerOnly') });
  return null;
}

async function demoSave(fields: FormData, locale: Locale) {
  const save = parsePoweredSave(fields);
  if (save.error !== null) return invalidSave(locale, save.error);
  return { ok: true, rev: (await import('#lib/server/demo-data')).demoPoweredSave(save.config) };
}

const refId = (fields: FormData) => Number(fields.get('id')) || 0;

const demoActions = {
  save: async ({ locals: { locale }, request }) => demoSave(await request.formData(), locale),
  add: async () => ({ id: 1 }),
  rotate: async () => (await import('#lib/server/demo-data')).demoOverlayRotate(),
  delete: async () => ({ deleted: true })
} satisfies Actions;

const previewOrigin = env.OVERLAY_ORIGIN ?? POWERED_ORIGIN;

const demoLoad = (async () => {
  const demo = await import('#lib/server/demo-data');
  return { readOnly: false, previewOrigin, overlays: demo.demoOverlayList(), powered: demo.demoPoweredView() };
}) satisfies PageServerLoad;

async function loadPowered(session: Session, overlays: OverlayView[]) {
  const system = overlays.find((overlay) => overlay.template === 'powered');
  if (!system) return null;
  return bestEffort(overlayGet(session, system.id), null);
}

const liveLoad = (async ({ locals: { session } }) => {
  if (!session) throw redirect(302, '/login?next=%2Foverlays');
  if (!hasOwnerAccess(session)) return { readOnly: true, previewOrigin, overlays: null, powered: null };
  const overlays = await bestEffort(overlayList(session), null);
  return { readOnly: false, previewOrigin, overlays, powered: overlays && (await loadPowered(session, overlays)) };
}) satisfies PageServerLoad;

const baseLoad = DEMO ? demoLoad : liveLoad;

export const load: PageServerLoad = async (event) => {
  if ((await event.parent()).isAlpha) throw redirect(302, '/overlays/hub');
  return baseLoad(event);
};

async function linkAction(session: Session, locale: Locale, run: () => Promise<OverlayView>, audit: string) {
  try {
    const { id, url, revision } = await run();
    auditDashboardImpersonation(session, audit);
    return { id, url, revision };
  } catch {
    return fail(503, { listError: translate(locale, 'overlays.listFailed') });
  }
}

const liveActions = {
  save: async ({ locals: { session, locale }, request }) => {
    const refused = ownerRefusal(session, locale, 'error');
    if (refused || !session) return refused;

    const save = parsePoweredSave(await request.formData());
    if (save.error !== null) return invalidSave(locale, save.error);
    return persistPowered(session, save, locale);
  },
  add: async ({ locals: { session, locale }, setHeaders }) => {
    const refused = ownerRefusal(session, locale, 'listError');
    if (refused || !session) return refused;

    setHeaders({ 'cache-control': 'no-store' });
    return linkAction(session, locale, () => overlayAddTemplate(session, 'powered'), 'overlays:add_template');
  },
  rotate: async ({ locals: { session, locale }, request, setHeaders }) => {
    const refused = ownerRefusal(session, locale, 'listError');
    if (refused || !session) return refused;

    setHeaders({ 'cache-control': 'no-store' });
    const id = refId(await request.formData());
    return linkAction(session, locale, () => overlayRotate(session, id), 'overlays:rotate');
  },
  delete: async ({ locals: { session, locale }, request }) => {
    const refused = ownerRefusal(session, locale, 'listError');
    if (refused || !session) return refused;

    try {
      await overlayDelete(session, refId(await request.formData()));
      auditDashboardImpersonation(session, 'overlays:delete');
      return { deleted: true };
    } catch {
      return fail(503, { listError: translate(locale, 'overlays.listFailed') });
    }
  }
} satisfies Actions;

export const actions = DEMO ? demoActions : liveActions;
