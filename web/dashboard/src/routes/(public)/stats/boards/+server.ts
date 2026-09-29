// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { publicBoards } from '$lib/server/public-boards';
import { STATS_EDGE_HEADERS } from '../edge-headers';

export const GET: RequestHandler = async () =>
  json(await publicBoards(), { headers: STATS_EDGE_HEADERS });
