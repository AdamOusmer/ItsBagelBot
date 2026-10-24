// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { delegationAccess } from '$lib/server/services';
import { COOKIE, seal } from '$lib/server/session';
import { cookieOptions } from '$lib/server/cookies';
import { isOwnerSession } from '$lib/server/owner';

async function findGrant(userId: string, owner: string | null) {
  if (!owner) throw redirect(302, '/settings?e=access');
  const grants = await delegationAccess(userId).catch(() => {
    throw redirect(302, '/settings?e=accessretry');
  });
  const grant = grants.find((g) => g.owner_user_id === owner);
  if (!grant) throw redirect(302, '/settings?e=access');
  return grant;
}

// Normal sessions only: re-sealing a view-as session would drop its 1h cap and audit trail.
export const GET: RequestHandler = async ({ url, locals, cookies }) => {
  const s = locals.session;
  if (!isOwnerSession(s)) throw redirect(302, '/login');

  const owner = url.searchParams.get('owner');
  const grant = await findGrant(s.user_id, owner);

  const value = seal({
    user_id: s.user_id,
    login: s.login,
    display_name: s.display_name,
    role: 'streamer',
    // Keep sid, iat and expires_at: a new sid escapes revocation and a new iat extends the session.
    sid: s.sid,
    iat: s.iat,
    expires_at: s.expires_at,
    delegate_of: grant.owner_user_id,
    delegate_login: grant.owner_login,
    sections: grant.sections
  });
  cookies.set(COOKIE, value, cookieOptions(url, Math.max(1, s.expires_at - Math.floor(Date.now() / 1000))));

  throw redirect(302, grant.sections[0] ? `/${grant.sections[0]}` : '/');
};
