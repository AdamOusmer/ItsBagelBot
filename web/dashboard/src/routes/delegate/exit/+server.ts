// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { COOKIE, seal } from '$lib/server/session';
import { cookieOptions } from '$lib/server/cookies';

export const GET: RequestHandler = ({ url, locals, cookies }) => {
  const s = locals.session;
  if (s?.delegate_of) {
    const value = seal({
      user_id: s.user_id,
      login: s.login,
      display_name: s.display_name,
      role: 'streamer',
      // Keep sid, iat and expires_at: a new sid escapes revocation and a new iat extends the session.
      sid: s.sid,
      iat: s.iat,
      expires_at: s.expires_at
    });
    cookies.set(COOKIE, value, cookieOptions(url, Math.max(1, s.expires_at - Math.floor(Date.now() / 1000))));
  }
  throw redirect(302, '/');
};
