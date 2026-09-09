// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { error } from '@sveltejs/kit';
import { dev } from '$app/environment';
import { requireAdmin } from '$lib/server/access';
import { subscribeStatus, decode, type FeedEvent } from '$lib/server/feed';
import { STATUS_PREFIX } from '$lib/server/services';

const enc = new TextEncoder();
const DEMO = dev && process.env.DEMO === '1';

function sse(event: string, data: unknown): Uint8Array {
  return enc.encode(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
}

// SSE bridge from the ingress shard-lifecycle status subjects. This is a live,
// non-persistent wildcard subscription under `${prefix}.>`, not the Twitch
// EventSub payload stream. Heartbeats keep proxies from idling it out. Under
// DEMO=1 the broker may be absent, so it emits a synthetic feed instead.
export const GET: RequestHandler = async ({ locals }) => {
  if (!(await requireAdmin(locals.session))) throw error(403, 'forbidden');

  if (DEMO) {
    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(enc.encode(': connected\n\n'));
        let n = 0;
        const fixture = import('$lib/server/demo-data');
        // Guarded, like the live branch's heartbeat below: the fixture tick
        // resolves a dynamic import, so its write lands a microtask AFTER the
        // timer fired. cancel() clearing the interval therefore cannot stop the
        // write already in flight, and enqueueing on a closed controller throws
        // ERR_INVALID_STATE out of a timer callback -- which is unhandled, and
        // takes the whole dev server down. Navigating away from the feed page
        // did exactly that.
        const push = (chunk: Uint8Array) => {
          try {
            controller.enqueue(chunk);
          } catch {
            /* closed */
          }
        };
        const tick = setInterval(() => {
          fixture.then(({ demoFeedEvent }) => {
            push(sse('feed', demoFeedEvent(n, STATUS_PREFIX)));
            n++;
          });
        }, 3000);
        const hb = setInterval(() => push(enc.encode(': keepalive\n\n')), 20000);
        // @ts-expect-error stash for cancel
        controller._cleanup = () => {
          clearInterval(tick);
          clearInterval(hb);
        };
      },
      cancel() {
        // @ts-expect-error stash
        this._cleanup?.();
      }
    });
    return streamResponse(stream);
  }

  let sub: Awaited<ReturnType<typeof subscribeStatus>>;
  try {
    sub = await subscribeStatus(STATUS_PREFIX);
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(enc.encode(': connected\n\n'));
        controller.enqueue(
          sse('feed', {
            subject: `${STATUS_PREFIX}.stream.error`,
            label: 'stream.error',
            tone: 'down',
            payload: `status stream unavailable: ${message}`,
            time: new Date().toLocaleTimeString('en-GB', { hour12: false })
          } satisfies FeedEvent)
        );
      }
    });
    return streamResponse(stream);
  }

  const stream = new ReadableStream({
    async start(controller) {
      controller.enqueue(enc.encode(': connected\n\n'));
      const hb = setInterval(() => {
        try {
          controller.enqueue(enc.encode(': keepalive\n\n'));
        } catch {
          /* closed */
        }
      }, 20000);
      try {
        for await (const m of sub) {
          controller.enqueue(sse('feed', decode(STATUS_PREFIX, m.subject, m.data)));
        }
      } catch {
        /* subscription closed */
      } finally {
        clearInterval(hb);
      }
    },
    cancel() {
      sub.unsubscribe();
    }
  });
  return streamResponse(stream);
};

function streamResponse(stream: ReadableStream): Response {
  return new Response(stream, {
    headers: {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-store',
      Connection: 'keep-alive'
    }
  });
}
