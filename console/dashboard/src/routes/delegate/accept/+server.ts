import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { delegationGet } from '$lib/server/rpc';

// Entry point for an invitee opening a share link. Validates the token is real
// and unconsumed, stashes it in a short-lived HttpOnly cookie, then hands off to
// the normal Twitch login. The actual single-use binding happens in the OAuth
// callback once we know who logged in.
export const GET: RequestHandler = async ({ url, cookies }) => {
  const token = url.searchParams.get('t');
  if (!token) throw redirect(302, '/login?e=link');

  const view = await delegationGet(token);
  if (!view || view.consumed) throw redirect(302, '/login?e=link');

  cookies.set('pending_delegation', token, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: 600
  });

  throw redirect(302, '/auth/login');
};
