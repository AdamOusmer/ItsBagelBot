// Copyright (c) 2026 Adam Ousmer. All rights reserved.

// Shared bits of the Spotify connect flow. Both ends of it — /spotify/connect
// and /spotify/callback — opened with the same three-step preamble (module
// gate, session or login redirect, demo short-circuit) and each carried its own
// copy of the state cookie name and the failure redirect. Two spellings of one
// contract in two files is how the pair drifts: a cookie renamed on one side
// silently stops matching on the other.
import { redirect } from '@sveltejs/kit';
import { gateModulePage } from '$lib/server/module-gate';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

const DEMO = dev && env.DEMO === '1';

/**
 * CSRF guard for the flow. Spotify ships no ID Token to bind, so this cookie IS
 * the replay guard, and both routes must agree on its name.
 */
export const SPOTIFY_STATE_COOKIE = 'spotify_oauth_state';

/**
 * Ten minutes is generous for "click, consent on your phone, come back";
 * anything older is treated as expired rather than honored.
 */
export const SPOTIFY_STATE_TTL_SECONDS = 600;

/**
 * Resolve the broadcaster this request acts for, or redirect. Never returns for
 * a caller that may not proceed: not signed in goes to login, and a demo
 * session bounces back to the page because there is no real account to connect.
 */
export function requireSongqueueActor(locals: App.Locals): string {
  gateModulePage(locals.session, 'songqueue');
  const uid = !DEMO && !locals.session ? null : effectiveId(locals.session);
  if (!uid) throw redirect(302, '/login?next=/songqueue');
  if (DEMO) throw redirect(302, '/songqueue');
  return uid;
}

/**
 * Abandon the flow with a reason the page renders as an explainer.
 *
 * Never call this inside a `try` whose `catch` also fails: it works by throwing
 * a redirect, so a surrounding catch swallows it and reports the catch's reason
 * instead. Decide first, then throw outside the try.
 */
export function songqueueFail(slug: string): never {
  throw redirect(302, `/songqueue?e=${slug}`);
}
