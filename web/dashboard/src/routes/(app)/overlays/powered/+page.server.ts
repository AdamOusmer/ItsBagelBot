// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail, redirect } from '@sveltejs/kit';
import { env } from '@bagel/kit/server/env';
import { translate, type Locale } from '@bagel/kit/i18n';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { RpcError } from '@bagel/kit/server/nats';
import {
  MAX_EVERY_MINUTES, MAX_SHOW_SECONDS, MIN_EVERY_MINUTES, MIN_SHOW_SECONDS, POWERED_ORIGIN, type PoweredConfigError
} from '@bagel/kit/overlays';
import { alphaActions, alphaOnly } from '#lib/server/alpha';
import { hasOwnerAccess } from '#lib/server/owner';
import { auditDashboardImpersonation } from '#lib/server/services/audit';
import { overlayGet, overlayList, overlaySave } from '#lib/server/services/overlays';
import type { Session } from '#lib/server/session';
import { ownerRefusal, rotateOverlay } from '../hub/overlay-actions';
import { parsePoweredSave } from '../powered-form';
import type { Actions, PageServerLoad } from './$types';

const HUB = '/overlays/hub';

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

const previewOrigin = env.OVERLAY_ORIGIN ?? POWERED_ORIGIN;

const liveLoad = (async ({ locals: { session } }) => {
  if (!session) throw redirect(302, '/login?next=%2Foverlays%2Fpowered');
  if (!hasOwnerAccess(session)) throw redirect(302, HUB);
  const overlays = await bestEffort(overlayList(session), null);
  if (!overlays) return { previewOrigin, powered: null };
  const system = overlays.find((overlay) => overlay.template === 'powered');
  if (!system) throw redirect(302, HUB);
  return { previewOrigin, powered: await bestEffort(overlayGet(session, system.id), null) };
}) satisfies PageServerLoad;

export const load: PageServerLoad = alphaOnly(liveLoad);

const liveActions = {
  save: async ({ locals: { session, locale }, request }) => {
    const refused = ownerRefusal(session, locale, 'error');
    if (refused || !session) return refused;

    const save = parsePoweredSave(await request.formData());
    if (save.error !== null) return invalidSave(locale, save.error);
    return persistPowered(session, save, locale);
  },
  rotate: rotateOverlay
} satisfies Actions;

export const actions = alphaActions(liveActions);
