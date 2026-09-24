// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { subscribe } from '$lib/server/live-hub';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

const DEMO = dev && env.DEMO === '1';

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

      send(': connected\n\n');
      send('event: ready\ndata: 1\n\n');

      const unsubscribe = subscribe(boardId, () => {
        send('event: invalidate\ndata: 1\n\n');
      });

      const ping = setInterval(() => send(': ping\n\n'), 25000);

      cleanup = () => {
        if (closed) return;
        closed = true;
        clearInterval(ping);
        unsubscribe();
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
