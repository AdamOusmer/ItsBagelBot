import type { RequestHandler } from './$types';
import { error } from '@sveltejs/kit';
import { allowed, isDemo, demoSession } from '$lib/server/access';
import { subscribeStatus, decode, type FeedEvent } from '$lib/server/feed';
import { STATUS_PREFIX } from '$lib/server/rpc';

const enc = new TextEncoder();

function sse(event: string, data: unknown): Uint8Array {
  return enc.encode(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
}

// SSE bridge from the ingress status subjects. Mirrors the old admin /events:
// a wildcard subscription under `${prefix}.>` forwarded as named events, with a
// heartbeat so proxies do not idle the connection out. Under DEMO=1 the broker
// may be absent, so it emits a synthetic feed instead of failing.
export const GET: RequestHandler = async ({ locals }) => {
  let s = locals.session;
  if (!s && isDemo()) s = demoSession;
  if (!allowed(s)) throw error(403, 'forbidden');

  if (isDemo()) {
    const stream = new ReadableStream({
      start(controller) {
        controller.enqueue(enc.encode(': connected\n\n'));
        let n = 0;
        const tones: FeedEvent['tone'][] = ['up', 'neutral', 'down'];
        const tick = setInterval(() => {
          const ev: FeedEvent = {
            subject: `${STATUS_PREFIX}.shard.${n % 4}.${tones[n % 3] === 'up' ? 'up' : tones[n % 3] === 'down' ? 'down' : 'keepalive'}`,
            label: `shard.${n % 4}`,
            tone: tones[n % 3],
            payload: `demo event #${n}`,
            time: new Date().toLocaleTimeString('en-GB', { hour12: false })
          };
          controller.enqueue(sse('feed', ev));
          n++;
        }, 3000);
        const hb = setInterval(() => controller.enqueue(enc.encode(': keepalive\n\n')), 20000);
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

  const sub = await subscribeStatus(STATUS_PREFIX);
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
