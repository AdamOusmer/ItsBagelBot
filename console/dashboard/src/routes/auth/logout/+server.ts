// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { COOKIE } from '$lib/server/session';
import { revokeSession } from '@bagel/shared/server/session-revocation';

export const POST: RequestHandler = async ({ cookies, url, locals }) => {
  const s = locals.session;
  // Revoke server-side before wiping the cookie: a copy of this cookie taken
  // off a shared/guest machine must die here too, not just forget itself in
  // this one browser. A session sealed before sid existed has nothing to
  // target — cookie deletion alone is still all that session ever had.
  if (s?.sid) {
    const now = Math.floor(Date.now() / 1000);
    await revokeSession(s.sid, s.expires_at - now);
  }
  cookies.delete(COOKIE, { path: '/', secure: url.protocol === 'https:' });
  throw redirect(302, '/login');
};
