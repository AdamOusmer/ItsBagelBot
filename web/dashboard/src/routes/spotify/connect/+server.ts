// Copyright (c) 2026 Adam Ousmer. All rights reserved.

import type { RequestHandler } from './$types';
import { redirect } from '@sveltejs/kit';
import { generateState } from '@bagel/kit/server/oauth';
import { spotifyAuthorizeURL, spotifyConfigured } from '$lib/server/oauth';
import { spotifyStore } from '$lib/server/spotify-store';
import {
  SPOTIFY_STATE_COOKIE,
  SPOTIFY_STATE_TTL_SECONDS,
  requireSongqueueActor
} from '$lib/server/spotify-oauth';

export const GET: RequestHandler = async ({ cookies, url, locals }) => {
  const uid = requireSongqueueActor(locals);

  if (!spotifyConfigured()) throw redirect(302, '/songqueue?e=unconfigured');

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
