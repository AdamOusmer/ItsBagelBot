import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { COOKIE, seal } from '$lib/server/session';

const SESSION_TTL = 7 * 24 * 3600;

// Leave a delegated session and return to the user's OWN dashboard. Re-seals a
// normal session for themselves (dropping the delegate fields) rather than
// logging out entirely. A non-delegate just goes home.
export const GET: RequestHandler = ({ url, locals, cookies }) => {
  const s = locals.session;
  if (s?.delegate_of) {
    const value = seal({
      user_id: s.user_id,
      login: s.login,
      display_name: s.display_name,
      role: 'streamer',
      expires_at: Math.floor(Date.now() / 1000) + SESSION_TTL
    });
    cookies.set(COOKIE, value, {
      path: '/',
      httpOnly: true,
      secure: url.protocol === 'https:',
      sameSite: 'lax',
      maxAge: SESSION_TTL
    });
  }
  throw redirect(302, '/');
};
