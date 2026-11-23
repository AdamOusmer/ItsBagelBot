// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { fail, redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { translate, type Locale } from '@bagel/kit/i18n';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { RpcError } from '@bagel/kit/server/nats';
import { validateDocument } from '@bagel/kit/overlay-document';
import { hasOwnerAccess } from '#lib/server/owner';
import { auditDashboardImpersonation } from '#lib/server/services/audit';
import { currentOnConflict, overlayCreateCustom, overlayGetCustom, overlaySaveCustom } from '#lib/server/services/overlays';
import type { Session } from '#lib/server/session';
import type { Actions, PageServerLoad } from './$types';

const DEMO = dev && env.DEMO === '1';

const REFUSALS: Record<string, { status: number; key: 'conflict' | 'invalid' }> = {
  conflict: { status: 409, key: 'conflict' },
  invalid: { status: 400, key: 'invalid' }
};

const requestedId = (url: URL) => Number(url.searchParams.get('id')) || 0;

const demoLoad = (async () => ({ readOnly: false, overlay: null, missing: false })) satisfies PageServerLoad;

const liveLoad = (async ({ locals: { session }, url }) => {
  if (!session) throw redirect(302, '/login?next=%2Foverlays%2Fcustom');
  if (!hasOwnerAccess(session)) return { readOnly: true, overlay: null, missing: false };
  const id = requestedId(url);
  const overlay = id ? await bestEffort(overlayGetCustom(session, id), null) : null;
  return { readOnly: false, overlay, missing: id > 0 && overlay === null };
}) satisfies PageServerLoad;

export const load: PageServerLoad = DEMO ? demoLoad : liveLoad;

function refusal(err: unknown, locale: Locale) {
  const known: (typeof REFUSALS)[string] | undefined = REFUSALS[err instanceof RpcError ? err.code : ''];
  const current = currentOnConflict(err);
  if (current) return fail(409, { error: translate(locale, 'overlayEditor.save.conflict'), current });
  if (known) return fail(known.status, { error: translate(locale, `overlayEditor.save.${known.key}`) });
  return fail(503, { error: translate(locale, 'overlayEditor.save.failed') });
}

function readSave(fields: FormData) {
  try {
    const checked = validateDocument(JSON.parse(String(fields.get('document'))));
    return checked.ok ? { id: Number(fields.get('id')) || 0, document: checked.doc } : null;
  } catch {
    return null;
  }
}

async function persist(session: Session, locale: Locale, fields: FormData) {
  const save = readSave(fields);
  if (!save) return fail(400, { error: translate(locale, 'overlayEditor.save.invalid') });
  if (DEMO) return { ok: true, id: save.id || 1, revision: save.document.rev + 1, url: save.id ? undefined : 'https://overlay.itsbagelbot.com/o#demo-custom-link' };
  try {
    const view = save.id
      ? await overlaySaveCustom(session, { ...save, name: save.document.name, revision: save.document.rev })
      : await overlayCreateCustom(session, { document: save.document });
    auditDashboardImpersonation(session, save.id ? 'overlays:custom_save' : 'overlays:custom_create');
    return { ok: true, id: view.id, revision: view.revision, url: view.url };
  } catch (err) {
    return refusal(err, locale);
  }
}

export const actions = {
  save: async ({ locals: { session, locale }, request }) => {
    if (!session) return fail(401, { error: translate(locale, 'serverErrors.notSignedIn') });
    if (!hasOwnerAccess(session)) return fail(403, { error: translate(locale, 'serverErrors.ownerOnly') });
    return persist(session, locale, await request.formData());
  }
} satisfies Actions;
