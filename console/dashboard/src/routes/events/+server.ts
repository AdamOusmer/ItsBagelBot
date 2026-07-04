import type { RequestHandler } from './$types';
import { subscribe } from '$lib/server/live-hub';
import { env } from '$env/dynamic/private';

// Server-sent events stream of cache-invalidation scopes for the signed-in
// user's board. The browser opens one EventSource (see (app)/+layout.svelte);
// when a Go write invalidates this user's state (status / commands / modules /
// grant / …) the same bus that evicts the server cache pushes the scope here and
// the client re-fetches. No polling.
//
// Owner or delegate: we stream the effective board id (delegate_of ?? user_id),
// matching the id the reads are keyed on.
export const GET: RequestHandler = ({ locals, request }) => {
  const s = locals.session;
  // DEMO has no real session; stream a demo board so the plumbing is exercisable
  // locally (no NATS events arrive, but the connection + keepalive do).
  const boardId = s ? (s.delegate_of ?? s.user_id) : env.DEMO === '1' ? 'demo' : null;
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
          closed = true; // controller already closed by a disconnect
        }
      };

      // Open the stream, then signal readiness. The client reconciles once on
      // every (re)connect, so a burst missed while briefly disconnected is
      // recovered without polling.
      send(': connected\n\n');
      send('event: ready\ndata: 1\n\n');

      const unsubscribe = subscribe(boardId, (scope) => {
        send(`event: invalidate\ndata: ${scope}\n\n`);
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
