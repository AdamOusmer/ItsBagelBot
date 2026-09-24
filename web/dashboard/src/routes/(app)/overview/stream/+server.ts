// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { overviewLanes } from '$lib/server/overview-lanes';
import { broadcastResponse, createBroadcast } from '$lib/server/sse-broadcast';

const DEMO = dev && env.DEMO === '1';

const TICK_MS = 5000;

const overview = createBroadcast<string>(TICK_MS, [
  async (boardId) => {
    const lanes = overviewLanes(boardId);
    const [stream, counters, volume, feed, answered] = await Promise.all([
      lanes.stream,
      lanes.counters,
      lanes.volume,
      lanes.feed,
      lanes.answered
    ]);
    return `event: live\ndata: ${JSON.stringify({ stream, counters, volume, feed, answered })}\n\n`;
  }
]);

export const GET: RequestHandler = ({ locals, request }) => {
  const s = locals.session;
  const boardId = s && !s.delegate_of ? s.user_id : DEMO ? 'demo' : null;
  if (!boardId) return new Response('unauthorized', { status: 401 });
  return broadcastResponse(request, overview, boardId);
};
