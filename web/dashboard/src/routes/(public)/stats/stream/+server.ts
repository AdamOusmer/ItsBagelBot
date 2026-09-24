// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { publicStats } from '$lib/server/public-stats';
import { publicBoards } from '$lib/server/public-boards';
import { broadcastResponse, createBroadcast } from '$lib/server/sse-broadcast';

const TICK_MS = 2000;

const stats = createBroadcast<'global'>(TICK_MS, [
  async () => `data: ${JSON.stringify(await publicStats())}\n\n`,
  async () => `event: boards\ndata: ${JSON.stringify(await publicBoards())}\n\n`
]);

export const GET: RequestHandler = ({ request }) => broadcastResponse(request, stats, 'global');
