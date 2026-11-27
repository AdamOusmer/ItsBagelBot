// Copyright (c) 2026 Adam Ousmer. All rights reserved.

import { redirect } from '@sveltejs/kit';
import { gateModulePage } from '#lib/server/module-gate';
import { effectiveId } from '#lib/server/board';

export const SPOTIFY_STATE_COOKIE = 'spotify_oauth_state';

export const SPOTIFY_STATE_TTL_SECONDS = 600;

export function requireSongqueueActor(locals: App.Locals): string {
  gateModulePage(locals.session, 'songqueue');
  if (!locals.session) throw redirect(302, '/login?next=/songqueue');
  return effectiveId(locals.session);
}

export function songqueueFail(slug: string): never {
  throw redirect(302, `/songqueue?e=${slug}`);
}
