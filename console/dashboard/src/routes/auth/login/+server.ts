import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { generateState } from 'arctic';
import { randomBytes } from 'node:crypto';
import { twitch, scopes, safeNextPath } from '$lib/server/oauth';

// Start of the Twitch authorization-code flow. State is stored in a short-lived
// HttpOnly cookie and verified in the callback (CSRF protection for OAuth).
// Nonce is also generated and stored: arctic's Twitch class does not accept a
// nonce arg, so we append it directly to the URL and verify claims.nonce in the
// callback (replay / id_token substitution guard).
export const GET: RequestHandler = ({ cookies, url }) => {
  const state = generateState();
  const nonce = randomBytes(16).toString('base64url');
  const authUrl = twitch().createAuthorizationURL(state, scopes());
  authUrl.searchParams.set('nonce', nonce);

  const cookieOpts = {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax' as const,
    maxAge: 600
  };

  cookies.set('oauth_state', state, cookieOpts);
  cookies.set('oauth_nonce', nonce, cookieOpts);

  // Where to land after the callback (e.g. /billing?subscribe=1 from the
  // pricing page). Rides its own short-lived cookie, same as state/nonce.
  const next = safeNextPath(url.searchParams.get('next'));
  if (next) cookies.set('login_next', next, cookieOpts);
  else cookies.delete('login_next', { path: '/' });

  throw redirect(302, authUrl.toString());
};
