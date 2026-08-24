// Copyright (c) 2026 Adam Ousmer. All rights reserved.

// Start of the Spotify connect flow: mint a state, park it in an HttpOnly
// cookie (CSRF — Spotify ships no ID Token to bind, so this cookie IS the
// replay guard), and bounce the broadcaster to accounts.spotify.com. The
// callback verifies the cookie before spending the code.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { generateState } from '@bagel/shared/server/oauth';
import { spotify, spotifyScopes } from '$lib/server/oauth';
import { gateModulePage } from '$lib/server/module-gate';
import { effectiveId } from '$lib/server/board';
import type { Session } from '$lib/server/session';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

const DEMO = dev && env.DEMO === '1';

// Ten minutes is generous for "click, consent on your phone, come back";
// anything older is treated as expired rather than honored.
const STATE_COOKIE = 'spotify_oauth_state';
const STATE_TTL_SECONDS = 600;

function gate(session: Session | null | undefined): void {
  gateModulePage(session, 'songqueue');
}

function requireSession(locals: App.Locals): string | null {
  if (!DEMO && !locals.session) return null;
  return effectiveId(locals.session);
}

export const GET: RequestHandler = async ({ cookies, url, locals }) => {
  gate(locals.session);
  if (!requireSession(locals)) throw redirect(302, '/login?next=/songqueue');
  if (DEMO) throw redirect(302, '/songqueue');

  // Unconfigured deployments land back on the page with an explainer instead
  // of throwing: the button is visible whenever Spotify is not connected, and
  // a missing env is a deployment state, not a server fault.
  if (!env.SPOTIFY_CLIENT_ID || !env.SPOTIFY_CLIENT_SECRET || !env.SPOTIFY_REDIRECT_URI) {
    throw redirect(302, '/songqueue?e=unconfigured');
  }

  const state = generateState();
  cookies.set(STATE_COOKIE, state, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: STATE_TTL_SECONDS
  });

  throw redirect(302, spotify().createAuthorizationURL(state, spotifyScopes()).toString());
};
