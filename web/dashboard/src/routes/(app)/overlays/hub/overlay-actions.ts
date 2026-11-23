// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail, type RequestEvent } from '@sveltejs/kit';
import { translate, type Locale } from '@bagel/kit/i18n';
import { hasOwnerAccess } from '#lib/server/owner';
import { auditDashboardImpersonation } from '#lib/server/services/audit';
import { overlayRotate } from '#lib/server/services/overlays';
import type { OverlayView } from '#lib/server/services/overlays';
import type { Session } from '#lib/server/session';

export function ownerRefusal(session: Session | null, locale: Locale, key: 'error' | 'listError') {
  if (!session) return fail(401, { [key]: translate(locale, 'serverErrors.notSignedIn') });
  if (!hasOwnerAccess(session)) return fail(403, { [key]: translate(locale, 'serverErrors.ownerOnly') });
  return null;
}

export const refId = (fields: FormData) => Number(fields.get('id')) || 0;

export async function linkAction(session: Session, locale: Locale, run: () => Promise<OverlayView>, audit: string) {
  try {
    const { id, url, revision } = await run();
    auditDashboardImpersonation(session, audit);
    return { id, url, revision };
  } catch {
    return fail(503, { listError: translate(locale, 'overlays.listFailed') });
  }
}

export async function rotateOverlay({ locals: { session, locale }, request, setHeaders }: RequestEvent) {
  const refused = ownerRefusal(session, locale, 'listError');
  if (refused || !session) return refused;

  setHeaders({ 'cache-control': 'no-store' });
  const id = refId(await request.formData());
  return linkAction(session, locale, () => overlayRotate(session, id), 'overlays:rotate');
}
