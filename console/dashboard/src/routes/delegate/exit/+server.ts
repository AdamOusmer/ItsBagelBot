import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { COOKIE, seal } from '$lib/server/session';

// Leave a delegated session and return to the user's OWN dashboard. Re-seals a
// normal session for themselves (dropping the delegate fields) rather than
// logging out entirely. A non-delegate just goes home.
//
// The re-seal keeps the original iat/expires_at: leaving a board must never
// extend a session's lifetime — only a fresh OAuth login does that.
export const GET: RequestHandler = ({ url, locals, cookies }) => {
  const s = locals.session;
  if (s?.delegate_of) {
    const value = seal({
      user_id: s.user_id,
      login: s.login,
      display_name: s.display_name,
      role: 'streamer',
      iat: s.iat,
      expires_at: s.expires_at
    });
    cookies.set(COOKIE, value, {
      path: '/',
      httpOnly: true,
      secure: url.protocol === 'https:',
      sameSite: 'lax',
      maxAge: Math.max(1, s.expires_at - Math.floor(Date.now() / 1000))
    });
  }
  throw redirect(302, '/');
};
