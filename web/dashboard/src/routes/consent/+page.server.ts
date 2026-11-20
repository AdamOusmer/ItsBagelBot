// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { redirect } from '@sveltejs/kit';
import { safeReturnPath } from '@bagel/kit/return-path';
import { consentFields, currentAccount, needsTerms, submitTerms } from '#lib/server/consent';
import { isOwnerSession } from '#lib/server/owner';
import type { Actions, PageServerLoad } from './$types';

function returnTo(url: URL): string {
  const next = safeReturnPath(url.searchParams.get('next')) ?? '/';
  const path = next.split(/[?#]/, 1)[0]?.replace(/\/$/, '');
  return path === '/consent' ? '/' : next;
}

export const load: PageServerLoad = async ({ locals, url }) => {
  const { session } = locals;
  const next = returnTo(url);
  if (!session) throw redirect(302, `/login?next=${encodeURIComponent(`/consent?next=${encodeURIComponent(next)}`)}`);
  if (!isOwnerSession(session)) throw redirect(303, '/');
  const account = currentAccount(locals);
  if (account && !needsTerms(account.termsVersion)) throw redirect(303, next);
  return consentFields(session);
};

export const actions: Actions = {
  default: async ({ locals, request, url }) => {
    const result = await submitTerms({ locals, request }, 'consent');
    if (!('uid' in result)) return result;
    throw redirect(303, returnTo(url));
  }
};
