import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { delegationAccess } from '$lib/server/services';
import { COOKIE, seal } from '$lib/server/session';

const SESSION_TTL = 7 * 24 * 3600;

// Open a dashboard that was shared with the signed-in user. Requires a NORMAL
// (non-delegate) session, verifies the grant still exists, then swaps the cookie
// for a section-limited delegate session over the owner's board.
export const GET: RequestHandler = async ({ url, locals, cookies }) => {
  const s = locals.session;
  if (!s || s.delegate_of) throw redirect(302, '/login');

  const owner = url.searchParams.get('owner');
  if (!owner) throw redirect(302, '/settings?e=access');

  let grants: Awaited<ReturnType<typeof delegationAccess>> = [];
  try {
    grants = await delegationAccess(s.user_id);
  } catch {
    throw redirect(302, '/settings?e=access');
  }

  const grant = grants.find((g) => g.owner_user_id === owner);
  if (!grant) throw redirect(302, '/settings?e=access');

  const value = seal({
    user_id: s.user_id,
    login: s.login,
    display_name: s.display_name,
    role: 'streamer',
    expires_at: Math.floor(Date.now() / 1000) + SESSION_TTL,
    delegate_of: owner,
    delegate_login: grant.owner_login,
    sections: grant.sections
  });
  cookies.set(COOKIE, value, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: SESSION_TTL
  });

  throw redirect(302, grant.sections[0] ? `/${grant.sections[0]}` : '/');
};
