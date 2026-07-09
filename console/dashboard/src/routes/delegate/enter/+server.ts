import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { delegationAccess } from '$lib/server/services';
import { COOKIE, seal } from '$lib/server/session';

// Open a dashboard that was shared with the signed-in user. Requires a NORMAL
// (non-delegate, non-impersonated) session, verifies the grant still exists,
// then swaps the cookie for a section-limited delegate session over the
// owner's board. An admin "view as" session is refused: swapping it here would
// launder the 1h impersonation into a session without the impersonator_*
// fields, losing both the short cap and the audit trail.
//
// The re-seal keeps the original iat/expires_at: switching boards must never
// extend a session's lifetime — only a fresh OAuth login does that.
export const GET: RequestHandler = async ({ url, locals, cookies }) => {
  const s = locals.session;
  if (!s || s.delegate_of || s.impersonator_id) throw redirect(302, '/login');

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
    iat: s.iat,
    expires_at: s.expires_at,
    delegate_of: owner,
    delegate_login: grant.owner_login,
    sections: grant.sections
  });
  cookies.set(COOKIE, value, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: Math.max(1, s.expires_at - Math.floor(Date.now() / 1000))
  });

  throw redirect(302, grant.sections[0] ? `/${grant.sections[0]}` : '/');
};
