// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Start of the YouTube connect flow: mint a state, park it in an HttpOnly
// cookie (CSRF — Google ships no ID Token for this scope set, so this cookie
// IS the replay guard), and bounce the broadcaster to accounts.google.com. The
// callback verifies the cookie before spending the code. Owner-only: the grant
// is stored against the account owner, and the only surface that links here
// (settings) is owner-only too, so a delegate can never bind their own Google
// account to someone else's channel.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { generateState } from '@bagel/shared/server/oauth';
import { google, youtubeScopes } from '$lib/server/oauth';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

const DEMO = dev && env.DEMO === '1';

// Ten minutes is generous for "click, consent on your phone, come back";
// anything older is treated as expired rather than honored.
const STATE_COOKIE = 'youtube_oauth_state';
const STATE_TTL_SECONDS = 600;

export const GET: RequestHandler = ({ cookies, url, locals }) => {
  if (!locals.session || locals.session.delegate_of) throw redirect(302, '/settings');
  if (DEMO) throw redirect(302, '/settings');

  // Unconfigured deployments land back on the page with an explainer instead
  // of throwing: the button is visible whenever YouTube is not connected, and
  // a missing env is a deployment state, not a server fault.
  if (!env.GOOGLE_CLIENT_ID || !env.GOOGLE_CLIENT_SECRET || !env.GOOGLE_REDIRECT_URI) {
    throw redirect(302, '/settings?youtube=unconfigured');
  }

  const state = generateState();
  cookies.set(STATE_COOKIE, state, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: STATE_TTL_SECONDS
  });

  throw redirect(302, google().createAuthorizationURL(state, youtubeScopes()).toString());
};
