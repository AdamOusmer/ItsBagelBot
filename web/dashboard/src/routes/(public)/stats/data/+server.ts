// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { publicStats } from '$lib/server/public-stats';

export const GET: RequestHandler = async () =>
  json(await publicStats(), { headers: { 'cache-control': 'no-store' } });
