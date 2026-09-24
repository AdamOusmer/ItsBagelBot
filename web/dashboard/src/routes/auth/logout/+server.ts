// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { COOKIE } from '$lib/server/session';
import { revokeSession } from '@bagel/kit/server/session-revocation';

export const POST: RequestHandler = async ({ cookies, url, locals }) => {
  const s = locals.session;
  if (s?.sid) {
    const now = Math.floor(Date.now() / 1000);
    await revokeSession(s.sid, s.expires_at - now);
  }
  cookies.delete(COOKIE, { path: '/', secure: url.protocol === 'https:' });
  throw redirect(302, '/login');
};
