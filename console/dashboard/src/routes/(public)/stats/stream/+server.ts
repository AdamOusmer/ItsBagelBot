// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { publicStats } from '$lib/server/public-stats';

// Server-sent events stream of the public global counters for /stats. Same
// leak-safe shape as (app)/events (hoisted idempotent cleanup wired to the
// enqueue throw, the request abort and cancel()), with two differences: the
// page is public, so there is no session or board id to key on, and there is
// no live-hub subscription to push from — the counters are polled here, once
// per connection, and pushed down as whole snapshots.
//
// Cost: publicStats() reads one single-flighted cache key (POLICY.live), so N
// concurrent viewers on a pod still cost ~1 RPC pair per second, not N.
//
// No separate keepalive timer: a data frame every 2s already keeps the
// Cloudflare tunnel from reaping an idle connection. The data IS the ping.
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
          cleanup(); // controller already closed by a disconnect
        }
      };

      // One snapshot per tick, as a single `data:` JSON line. publicStats()
      // degrades rather than rejecting; the rejection arm is belt-and-braces so
      // a surprise there can never become an unhandled rejection on the server.
      const push = () =>
        publicStats().then(
          (stats) => send(`data: ${JSON.stringify(stats)}\n\n`),
          () => {}
        );

      send(': connected\n\n');
      // Don't make the first viewer wait a tick for numbers.
      void push();

      const timer = setInterval(() => void push(), TICK_MS);

      cleanup = () => {
        if (closed) return;
        closed = true;
        clearInterval(timer);
      };
      // Client navigated away / closed the tab.
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
      // Defeat proxy/response buffering so frames flush immediately.
      'X-Accel-Buffering': 'no'
    }
  });
};
