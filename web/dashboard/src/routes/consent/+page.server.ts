// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { redirect } from '@sveltejs/kit';
import { safeReturnPath } from '@bagel/kit/return-path';
import { consentFields, currentAccount, needsTerms, submitTerms } from '#lib/server/consent';
import { isOwnerSession } from '#lib/server/owner';
import type { Actions, PageServerLoad } from './$types';

const DEMO = dev && env.DEMO === '1';

async function consentSession(locals: App.Locals) {
  return locals.session ?? (DEMO ? (await import('#lib/server/demo-data')).demoSession() : null);
}

function returnTo(url: URL): string {
  const next = safeReturnPath(url.searchParams.get('next')) ?? '/';
  const path = next.split(/[?#]/, 1)[0]?.replace(/\/$/, '');
  return path === '/consent' ? '/' : next;
}

function loginPath(next: string): string {
  return `/login?next=${encodeURIComponent(`/consent?next=${encodeURIComponent(next)}`)}`;
}

function accepted(locals: App.Locals): boolean {
  const account = currentAccount(locals);
  return !!account && !needsTerms(account.termsVersion);
}

export const load: PageServerLoad = async ({ locals, url }) => {
  const session = await consentSession(locals);
  const next = returnTo(url);
  if (!session) throw redirect(302, loginPath(next));
  if (!isOwnerSession(session)) throw redirect(303, '/');
  if (accepted(locals)) throw redirect(303, next);
  return consentFields(session, locals, DEMO);
};

export const actions: Actions = {
  default: async ({ locals, request, url }) => {
    const session = await consentSession(locals);
    const result = await submitTerms({ locals: { ...locals, session }, request }, 'consent', DEMO);
    if (!('uid' in result)) return result;
    const next = returnTo(url);
    if (result.marketingSkipped) return { next, marketingSkipped: result.marketingSkipped };
    throw redirect(303, next);
  }
};
