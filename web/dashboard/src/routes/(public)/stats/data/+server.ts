// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { publicStats } from '$lib/server/public-stats';
import { STATS_EDGE_HEADERS } from '../edge-headers';

export const GET: RequestHandler = async () =>
  json(await publicStats(), { headers: STATS_EDGE_HEADERS });
