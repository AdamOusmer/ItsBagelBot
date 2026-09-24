// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { PageServerLoad } from './$types';
import { redirect } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { generateState } from '@bagel/kit/server/oauth';
import { randomBytes } from 'node:crypto';
import { twitch, scopes, safeNextPath } from '$lib/server/oauth';
import { skipAuthorizeIfSignedIn } from '$lib/server/oauth-start';

const DEMO = dev && env.DEMO === '1';

export const load: PageServerLoad = ({ cookies, url, locals }) => {
  if (DEMO) {
    throw redirect(302, safeNextPath(url.searchParams.get('next')) ?? '/');
  }

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

  const next = safeNextPath(url.searchParams.get('next'));
  if (next) cookies.set('login_next', next, cookieOpts);
  else cookies.delete('login_next', { path: '/' });

  throw redirect(302, authUrl.toString());
};
