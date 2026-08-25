// Copyright (c) 2026 Adam Ousmer. All rights reserved.

// Start of the Spotify connect flow: mint a state, park it in an HttpOnly
// cookie (CSRF — Spotify ships no ID Token to bind, so this cookie IS the
// replay guard), and bounce the broadcaster to accounts.spotify.com. The
// callback verifies the cookie before spending the code.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { generateState } from '@bagel/shared/server/oauth';
import { spotify, spotifyConfigured, spotifyScopes } from '$lib/server/oauth';
import {
  SPOTIFY_STATE_COOKIE,
  SPOTIFY_STATE_TTL_SECONDS,
  requireSongqueueActor
} from '$lib/server/spotify-oauth';

export const GET: RequestHandler = async ({ cookies, url, locals }) => {
  requireSongqueueActor(locals);

  // Unconfigured deployments land back on the page with an explainer instead
  // of throwing: the button is visible whenever Spotify is not connected, and
  // a missing env is a deployment state, not a server fault.
  if (!spotifyConfigured()) throw redirect(302, '/songqueue?e=unconfigured');

  const state = generateState();
  cookies.set(SPOTIFY_STATE_COOKIE, state, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: SPOTIFY_STATE_TTL_SECONDS
  });

  throw redirect(302, spotify().createAuthorizationURL(state, spotifyScopes()).toString());
};
