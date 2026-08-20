// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { publicBoards } from '$lib/server/public-boards';

// Lazy poll target for the /stats leaderboards: the same cached snapshot the
// SSR load returns, as JSON. The boards are a lifetime ranking behind a 15s
// fresh window, so the page asks far less often than it does for the odometer.
// Anonymous traffic is capped by traefik's per-IP rateLimit on the public host
// (the in-app limiter only meters sessions), and the snapshot itself is
// single-flighted behind one cache key, so a hot page costs at most one board
// read per fresh window per pod however many visitors it has.
export const GET: RequestHandler = async () =>
  json(await publicBoards(), { headers: { 'cache-control': 'no-store' } });
