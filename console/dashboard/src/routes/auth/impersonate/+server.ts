// Redeem an admin "view as" link: verify the signed token, then seal a short
// (1h) dashboard session for the target user that also carries the acting
// admin (impersonator_*). hooks.server.ts opens it like any session; the write
// actions audit back to the admin while these fields are present.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { verifyViewAs } from '@bagel/shared/server/impersonation';
import { COOKIE, seal } from '$lib/server/session';

const SESSION_TTL = 3600; // 1h — shorter than a normal login.

export const GET: RequestHandler = ({ url, cookies }) => {
  const token = url.searchParams.get('t') ?? '';
  const p = verifyViewAs(token);
  if (!p) throw redirect(302, '/login?e=imp');

  const value = seal({
    user_id: p.sub,
    login: p.login,
    display_name: p.display_name,
    role: 'streamer',
    expires_at: Math.floor(Date.now() / 1000) + SESSION_TTL,
    impersonator_id: p.by_id,
    impersonator_login: p.by_login
  });

  cookies.set(COOKIE, value, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: SESSION_TTL
  });

  throw redirect(302, '/');
};
