// Copyright (c) 2026 Adam Ousmer. All rights reserved.

import { redirect } from '@sveltejs/kit';
import { requireModuleOAuthActor } from './module-oauth';

export const SPOTIFY_STATE_COOKIE = 'spotify_oauth_state';

export const SPOTIFY_STATE_TTL_SECONDS = 600;

export function requireSongqueueActor(locals: App.Locals): string {
  return requireModuleOAuthActor(locals, { id: 'songqueue', path: '/songqueue' }).uid;
}

export function songqueueFail(slug: string): never {
  throw redirect(302, `/songqueue?e=${slug}`);
}
