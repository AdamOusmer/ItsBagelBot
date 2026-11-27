// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail, redirect } from '@sveltejs/kit';
import { translate } from '@bagel/kit/i18n';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { alphaActions, alphaOnly } from '#lib/server/alpha';
import { hasOwnerAccess } from '#lib/server/owner';
import { auditDashboardImpersonation } from '#lib/server/services/audit';
import { overlayAddTemplate, overlayDelete, overlayList } from '#lib/server/services/overlays';
import { linkAction, ownerRefusal, refId, rotateOverlay } from './overlay-actions';
import type { Actions, PageServerLoad } from './$types';

const liveLoad = (async ({ locals: { session } }) => {
  if (!session) throw redirect(302, '/login?next=%2Foverlays');
  if (!hasOwnerAccess(session)) return { readOnly: true, overlays: null };
  return { readOnly: false, overlays: await bestEffort(overlayList(session), null) };
}) satisfies PageServerLoad;

export const load: PageServerLoad = alphaOnly(liveLoad);

const liveActions = {
  add: async ({ locals: { session, locale }, setHeaders }) => {
    const refused = ownerRefusal(session, locale, 'listError');
    if (refused || !session) return refused;

    setHeaders({ 'cache-control': 'no-store' });
    return linkAction(session, locale, () => overlayAddTemplate(session, 'powered'), 'overlays:add_template');
  },
  rotate: rotateOverlay,
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

export const actions = alphaActions(liveActions);
