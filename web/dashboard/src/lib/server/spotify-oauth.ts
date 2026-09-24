// Copyright (c) 2026 Adam Ousmer. All rights reserved.

import { redirect } from '@sveltejs/kit';
import { gateModulePage } from '$lib/server/module-gate';
import { effectiveId } from '$lib/server/board';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

const DEMO = dev && env.DEMO === '1';

export const SPOTIFY_STATE_COOKIE = 'spotify_oauth_state';

export const SPOTIFY_STATE_TTL_SECONDS = 600;

export function requireSongqueueActor(locals: App.Locals): string {
  gateModulePage(locals.session, 'songqueue');
  const uid = !DEMO && !locals.session ? null : effectiveId(locals.session);
  if (!uid) throw redirect(302, '/login?next=/songqueue');
  if (DEMO) throw redirect(302, '/songqueue');
  return uid;
}

export function songqueueFail(slug: string): never {
  throw redirect(302, `/songqueue?e=${slug}`);
}
