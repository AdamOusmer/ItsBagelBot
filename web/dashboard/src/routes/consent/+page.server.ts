// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { env } from '@bagel/kit/server/env';
import { redirect } from '@sveltejs/kit';
import { safeReturnPath } from '@bagel/kit/return-path';
import { bestEffort } from '@bagel/kit/server/best-effort';
import { consentFields, currentAccount, needsTerms, submitTerms } from '#lib/server/consent';
import { isOwnerSession } from '#lib/server/owner';
import { freshAccountState, type AccountState } from '#lib/server/services';
import type { Session } from '#lib/server/session';
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

function skippedReason(url: URL) {
  const skipped = url.searchParams.get('skipped');
  return skipped === 'no_email' || skipped === 'unavailable' ? skipped : null;
}

async function accepted(locals: App.Locals, session: Session, checkFresh: boolean): Promise<boolean> {
  if (!needsTerms(currentAccount(locals)?.termsVersion)) return true;
  if (!checkFresh || DEMO) return false;
  const account = await bestEffort<AccountState | null>(freshAccountState(session.user_id), null);
  return !needsTerms(account?.termsVersion);
}

export const load: PageServerLoad = async ({ locals, url }) => {
  const session = await consentSession(locals);
  const next = returnTo(url);
  if (!session) throw redirect(302, loginPath(next));
  if (!isOwnerSession(session)) throw redirect(303, '/');

  const skipped = skippedReason(url);
  if (!(await accepted(locals, session, skipped !== null))) {
    return { consent: await consentFields(session, locals, DEMO), notice: null };
  }
  if (!skipped) throw redirect(303, next);
  return { consent: null, notice: { skipped, next } };
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
