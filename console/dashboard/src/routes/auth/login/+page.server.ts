// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { generateState } from '@bagel/shared/server/oauth';
import { randomBytes } from 'node:crypto';
import { twitch, scopes, safeNextPath } from '$lib/server/oauth';
import { skipAuthorizeIfSignedIn } from '$lib/server/oauth-start';

// Gated on the build-time `dev` constant first, so Rollup erases the demo
// branch from production builds.
const DEMO = dev && env.DEMO === '1';

// Start of the Twitch authorization-code flow. This is a *page* load (not a
// +server.ts endpoint) so a failed start renders src/routes/+error.svelte —
// endpoints never go through that boundary and SvelteKit paints its grey
// fallback instead ("500 | Internal Error").
//
// State is stored in a short-lived HttpOnly cookie and verified in the
// callback (CSRF protection for OAuth). Nonce is also generated and stored:
// the shared Twitch client does not accept a nonce arg, so we append it
// directly to the URL and verify claims.nonce in the callback.
export const load: PageServerLoad = ({ cookies, url, locals }) => {
  // DEMO has no Twitch app credentials and already synthesizes a session in
  // (app)/+layout.server.ts. Sending the visitor to Twitch would 500; send
  // them into the demo console instead, honouring ?next= when it is safe.
  if (DEMO) {
    throw redirect(302, safeNextPath(url.searchParams.get('next')) ?? '/');
  }

  // Signed-in marketing CTA: land in the console. Reconnect and delegate-accept
  // opt out of this skip (see skipAuthorizeIfSignedIn).
  if (
    skipAuthorizeIfSignedIn({
      hasSession: !!locals.session,
      pendingDelegation: cookies.get('pending_delegation'),
      reauth: url.searchParams.get('reauth')
    })
  ) {
    throw redirect(302, safeNextPath(url.searchParams.get('next')) ?? '/');
  }

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
