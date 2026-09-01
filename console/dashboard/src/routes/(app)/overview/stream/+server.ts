// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';
import { overviewLanes } from '$lib/server/overview-lanes';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// Server-sent events stream of the Overview's live panels (stream metadata,
// per-stream counters, chat volume, activity feed, answered-tonight).
//
// These lanes are the one part of the dashboard the cache-invalidation bus
// does not cover: nothing publishes an invalidation when a viewer count moves,
// a chat minute ticks over or the bot answers a command, so the layout's
// /events stream stays silent and the SSR snapshot would sit frozen until a
// manual reload. Same shape as (public)/stats/stream: the lanes are polled
// here, once per connection, and pushed down as whole snapshots. Everything
// else on the page stays invalidation-driven through /events.
//
// Cost: every lane is a fabric read under POLICY.live (1s fresh / 2s SWR,
// single-flight), so N tabs on a pod still cost ~1 read per lane per tick,
// not N. Each lane degrades to its own `ok: false` default rather than
// rejecting, so the combined snapshot below cannot become an unhandled
// rejection on the server.
//
// Owners only, for the same reason as /events: a delegate's grant does not
// cover the owner's stream telemetry, and the Overview never renders for a
// delegate anyway.
//
// No separate keepalive timer: a snapshot every 5s already keeps the
// Cloudflare tunnel from reaping an idle connection. The data IS the ping.
const TICK_MS = 5000;

export const GET: RequestHandler = ({ locals, request }) => {
  const s = locals.session;
  // DEMO has no real session; stream the demo fixtures so the plumbing is
  // exercisable locally.
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
          cleanup(); // controller already closed by a disconnect
        }
      };

      // One combined snapshot per tick. The lanes never reject (each resolves
      // to its degraded default on failure); the rejection arm is
      // belt-and-braces so a surprise can never become an unhandled rejection.
      const push = () => {
        const lanes = overviewLanes(boardId);
        void Promise.all([lanes.stream, lanes.counters, lanes.volume, lanes.feed, lanes.answered]).then(
          ([meta, counters, volume, feed, answered]) =>
            send(`event: live\ndata: ${JSON.stringify({ stream: meta, counters, volume, feed, answered })}\n\n`),
          () => {}
        );
      };

      send(': connected\n\n');
      // Don't make the page wait a tick for its first refresh after a
      // reconnect (the SSR snapshot already covered the initial load).
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
