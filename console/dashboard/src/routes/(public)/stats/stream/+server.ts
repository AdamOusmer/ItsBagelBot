// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { publicStats } from '$lib/server/public-stats';
import { publicBoards } from '$lib/server/public-boards';

// Server-sent events stream of the public global counters for /stats. Same
// leak-safe shape as (app)/events (hoisted idempotent cleanup wired to the
// enqueue throw, the request abort and cancel()), with two differences: the
// page is public, so there is no session or board id to key on, and there is
// no live-hub subscription to push from — the counters are polled here, once
// per connection, and pushed down as whole snapshots.
//
// Each tick carries both halves of the page: the global counters as the default
// `data:` frame, and the per-channel boards as a named `boards` event. The
// boards are a separate event rather than a second field so the counter frame's
// shape (and the client's odometer path) stays exactly what it was.
//
// Cost: publicStats() reads one single-flighted cache key (POLICY.live) and
// publicBoards() one more that is itself shared across pods through Valkey, so
// N concurrent viewers on a pod still cost ~1 read per tick, not N — and the
// boards cost the whole deployment one read per tick rather than one per pod.
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

      // One snapshot of each per tick. Both readers degrade rather than
      // rejecting; the rejection arms are belt-and-braces so a surprise in
      // either can never become an unhandled rejection on the server. They are
      // not awaited together: a slow board read must not delay the counters.
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
      // Don't make the first viewer wait a tick for numbers.
      push();

      const timer = setInterval(push, TICK_MS);

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
