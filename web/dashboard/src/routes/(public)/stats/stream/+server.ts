// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { publicStats } from '$lib/server/public-stats';
import { publicBoards } from '$lib/server/public-boards';

const TICK_MS = 2000;

export const GET: RequestHandler = ({ request }) => {
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
        void publicStats().then(
          (stats) => send(`data: ${JSON.stringify(stats)}\n\n`),
          () => {}
        );
        void publicBoards().then(
          (boards) => send(`event: boards\ndata: ${JSON.stringify(boards)}\n\n`),
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
