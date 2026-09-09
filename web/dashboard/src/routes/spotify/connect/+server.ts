// Copyright (c) 2026 Adam Ousmer. All rights reserved.

// Start of the Spotify connect flow: mint a state, park it in an HttpOnly
// cookie (CSRF: Spotify ships no ID Token to bind, so this cookie IS the
// replay guard), and bounce the broadcaster to accounts.spotify.com. The
// callback verifies the cookie before spending the code.
import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { generateState } from '@bagel/shared/server/oauth';
import { spotifyAuthorizeURL, spotifyConfigured } from '$lib/server/oauth';
import { spotifyStore } from '$lib/server/spotify-store';
import {
  SPOTIFY_STATE_COOKIE,
  SPOTIFY_STATE_TTL_SECONDS,
  requireSongqueueActor
} from '$lib/server/spotify-oauth';

export const GET: RequestHandler = async ({ cookies, url, locals }) => {
  const uid = requireSongqueueActor(locals);

  // Unconfigured deployments land back on the page with an explainer instead
  // of throwing: the button is visible whenever Spotify is not connected, and
  // a missing env is a deployment state, not a server fault.
  if (!spotifyConfigured()) throw redirect(302, '/songqueue?e=unconfigured');

  // The consent screen belongs to the BROADCASTER's own Spotify app, so a
  // channel that has not registered one has nothing to redirect to. Same
  // treatment as the missing env: an explainer, not a fault: this one is the
  // first setup step rather than a deployment gap.
  const app = await spotifyStore(uid).app();
  if (!app.present) throw redirect(302, '/songqueue?e=noapp');

  const state = generateState();
  cookies.set(SPOTIFY_STATE_COOKIE, state, {
    path: '/',
    httpOnly: true,
    secure: url.protocol === 'https:',
    sameSite: 'lax',
    maxAge: SPOTIFY_STATE_TTL_SECONDS
  });

  throw redirect(302, spotifyAuthorizeURL(app.clientId, state));
};
