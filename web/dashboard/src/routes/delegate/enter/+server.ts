// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { delegationAccess } from '$lib/server/services';
import { COOKIE, seal } from '$lib/server/session';

// Normal sessions only: re-sealing a view-as session would drop its 1h cap and audit trail.
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
    // Keep sid, iat and expires_at: a new sid escapes revocation and a new iat extends the session.
    sid: s.sid,
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
