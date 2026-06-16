import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { generateState } from 'arctic';
import { randomBytes } from 'node:crypto';
import { twitch, scopes } from '$lib/server/oauth';

// Start of the Twitch authorization-code flow. State + nonce are stored in
// short-lived HttpOnly cookies and verified in the callback (CSRF + id_token
// substitution guards). The browser drives the whole flow, so the tailnet-only
// callback host resolves fine: the operator is already on the tailnet.
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

  throw redirect(302, authUrl.toString());
};
