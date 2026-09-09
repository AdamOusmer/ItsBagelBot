// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { publicStats } from '$lib/server/public-stats';

// Poll target for the /stats page: the same cached snapshot the SSR load
// returns, as JSON. Anonymous callers already ride the shared IP-keyed read
// limiter in hooks.server.ts, and the snapshot itself is single-flighted behind
// one cache key, so a hot page costs at most ~1 RPC pair/second per pod.
export const GET: RequestHandler = async () =>
  json(await publicStats(), { headers: { 'cache-control': 'no-store' } });
