import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { generateState } from 'arctic';
import { twitch, scopes } from '$lib/server/oauth';

// Start of the Twitch authorization-code flow. State is stored in a short-lived
// HttpOnly cookie and verified in the callback (CSRF protection for OAuth).
export const GET: RequestHandler = ({ cookies, url }) => {
  const state = generateState();
  const authUrl = twitch().createAuthorizationURL(state, scopes());

  cookies.set('oauth_state', state, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: 600
  });

  throw redirect(302, authUrl.toString());
};
