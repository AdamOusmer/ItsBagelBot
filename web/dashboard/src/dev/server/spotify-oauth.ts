// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { redirect } from '@sveltejs/kit';

export * from '#lib/server/spotify-oauth';

export function requireSongqueueActor(): never {
  throw redirect(302, '/songqueue');
}
