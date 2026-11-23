// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { redirect } from '@sveltejs/kit';
import { consentFields, submitTerms } from '#lib/server/consent';
import { loginPath, returnTo, skippedReason, termsAccepted } from '#lib/server/consent-route';
import type { Session } from '#lib/server/session';
import type { Actions, PageServerLoad } from './$types';

const DEMO = dev && env.DEMO === '1';

async function consentSession(locals: App.Locals) {
  return locals.session ?? (DEMO ? (await import('#lib/server/demo-data')).demoSession() : null);
}

function checkFresh(skipped: string | null): boolean {
  return skipped !== null && !DEMO;
}

async function consentPage(locals: App.Locals, session: Session, url: URL) {
  const next = returnTo(url);
  const skipped = skippedReason(url);
  if (!(await termsAccepted(locals, session, checkFresh(skipped)))) {
    return { consent: await consentFields(locals, DEMO), notice: null };
  }
  if (!skipped) throw redirect(303, next);
  return { consent: null, notice: { skipped, next } };
}

export const load: PageServerLoad = async ({ locals, url }) => {
  const session = await consentSession(locals);
  if (!session) throw redirect(302, loginPath(returnTo(url)));
  if (session.impersonator_id) throw redirect(303, '/');
  return consentPage(locals, session, url);
};

export const actions: Actions = {
  default: async ({ locals, request, url }) => {
    const session = await consentSession(locals);
    const result = await submitTerms({ locals: { ...locals, session }, request }, 'consent', DEMO);
    if (!('uid' in result)) return result;
    const next = returnTo(url);
    if (result.marketingSkipped) {
      const query = new URLSearchParams({ skipped: result.marketingSkipped, next });
      throw redirect(303, `/consent?${query}`);
    }
    throw redirect(303, next);
  }
};
