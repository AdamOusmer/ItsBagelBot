// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { subscribe } from '$lib/server/live-hub';
import { dev } from '$app/environment';
import { env } from '$env/dynamic/private';

// Gated on the build-time `dev` constant first, so Rollup erases every demo
// branch (and the dynamic demo-data import inside it) from production builds.
const DEMO = dev && env.DEMO === '1';

// Server-sent events stream of cache-invalidation scopes for the signed-in
// user's board. The browser opens one EventSource (see (app)/+layout.svelte);
// when a Go write invalidates this user's state (status / commands / modules /
// grant / …) the same bus that evicts the server cache pushes the scope here and
// the client re-fetches. No polling.
//
// Owners only. A delegate's pages already SSR fresh on every navigation (the
// client never opens this stream for them), and the board they operate is not
// theirs: streaming it would hand them a change signal for families their
// grant does not cover — `ban:<owner>`, `billing-state:<owner>` — which is
// exactly the inference the section scoping exists to prevent.
export const GET: RequestHandler = ({ locals, request }) => {
  const s = locals.session;
  // DEMO has no real session; stream a demo board so the plumbing is exercisable
  // locally (no NATS events arrive, but the connection + keepalive do).
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

      // Open the stream, then signal readiness. The client reconciles once on
      // every (re)connect, so a burst missed while briefly disconnected is
      // recovered without polling.
      send(': connected\n\n');
      send('event: ready\ndata: 1\n\n');

      // The payload is a constant, not the scope name: the client refetches
      // everything on any invalidate (see (app)/+layout.svelte), so the scope
      // is dead data on the wire — and dead data that names which family of
      // the owner's state changed.
      const unsubscribe = subscribe(boardId, () => {
        send('event: invalidate\ndata: 1\n\n');
      });

      // Keepalive comment so idle proxies (the Cloudflare tunnel) don't reap the
      // connection mid-wait for a webhook.
      const ping = setInterval(() => send(': ping\n\n'), 25000);

      cleanup = () => {
        if (closed) return;
        closed = true;
        clearInterval(ping);
        unsubscribe();
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
      // Defeat proxy/response buffering so events flush immediately.
      'X-Accel-Buffering': 'no'
    }
  });
};
