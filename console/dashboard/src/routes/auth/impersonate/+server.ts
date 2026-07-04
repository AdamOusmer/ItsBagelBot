// Redeem an admin "view as" link: verify the signed token, then seal a short
// (1h) dashboard session for the target user that also carries the acting
// admin (impersonator_*). hooks.server.ts opens it like any session; the write
// actions audit back to the admin while these fields are present.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { verifyViewAs } from '@bagel/shared/server/impersonation';
import { COOKIE, seal } from '$lib/server/session';
import { isLocale, LOCALE_COOKIE } from '@bagel/shared/i18n';
import { userLocale } from '$lib/server/services';

const SESSION_TTL = 3600; // 1h — shorter than a normal login.

export const GET: RequestHandler = async ({ url, cookies }) => {
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

  try {
    const saved = await userLocale(p.sub);
    if (isLocale(saved)) {
      cookies.set(LOCALE_COOKIE, saved, {
        path: '/',
        httpOnly: true,
        secure: url.protocol === 'https:',
        sameSite: 'lax',
        maxAge: 60 * 60 * 24 * 365
      });
    }
  } catch (err) {
    /* best-effort */
  }

  throw redirect(302, '/');
};
