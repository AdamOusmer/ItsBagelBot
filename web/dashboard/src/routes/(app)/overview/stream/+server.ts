// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { overviewLanes } from '$lib/server/overview-lanes';

const DEMO = dev && env.DEMO === '1';

const TICK_MS = 5000;

export const GET: RequestHandler = ({ locals, request }) => {
  const s = locals.session;
  const boardId = s && !s.delegate_of ? s.user_id : DEMO ? 'demo' : null;
  if (!boardId) return new Response('unauthorized', { status: 401 });

  let cleanup = () => {};

  const stream = new ReadableStream({
    start(controller) {
      const enc = new TextEncoder();
      let closed = false;
      const send = (line: string) => {
        if (closed) return;
        try {
          controller.enqueue(enc.encode(line));
        } catch {
          cleanup();
        }
      };

      const push = () => {
        const lanes = overviewLanes(boardId);
        void Promise.all([lanes.stream, lanes.counters, lanes.volume, lanes.feed, lanes.answered]).then(
          ([meta, counters, volume, feed, answered]) =>
            send(`event: live\ndata: ${JSON.stringify({ stream: meta, counters, volume, feed, answered })}\n\n`),
          () => {}
        );
      };

      send(': connected\n\n');
      push();

      const timer = setInterval(push, TICK_MS);

      cleanup = () => {
        if (closed) return;
        closed = true;
        clearInterval(timer);
      };
      request.signal.addEventListener('abort', cleanup);
    },
    cancel() {
      cleanup();
    }
  });

  return new Response(stream, {
    headers: {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-store',
      Connection: 'keep-alive',
      'X-Accel-Buffering': 'no'
    }
  });
};
