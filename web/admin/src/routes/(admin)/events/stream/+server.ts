// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { RequestHandler } from './$types';
import { error } from '@sveltejs/kit';
import { logger } from '@bagel/kit/server/logger';
import { requireAdmin } from '#lib/server/access';
import { statusHistory } from '#lib/server/feed';
import type { FeedHistory } from '#lib/server/feed-history';
import { STATUS_PREFIX } from '#lib/server/services';

const enc = new TextEncoder();

type FeedSource = () => Promise<FeedHistory>;

function sse(event: 'history' | 'feed' | 'unavailable', data: unknown, id?: string): Uint8Array {
  const cursor = id ? `id: ${id}\n` : '';
  return enc.encode(`${cursor}event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
}

function startStream(controller: ReadableStreamDefaultController, source: FeedSource): () => void {
  let stopped = false;
  let unsubscribe: (() => void) | undefined;
  let heartbeat: ReturnType<typeof setInterval> | undefined;
  const stop = () => {
    stopped = true;
    clearInterval(heartbeat);
    unsubscribe?.();
  };
  const send = (data: Uint8Array) => {
    try {
      controller.enqueue(data);
    } catch {
      stop();
    }
  };
  const close = () => {
    stop();
    controller.close();
  };
  void source().then((history) => {
    if (stopped) return;
    unsubscribe = history.listen({
      history: (events) => send(sse('history', events, history.generation)),
      feed: (event) => send(sse('feed', event)),
      close
    });
    if (stopped) return unsubscribe();
    const keepalive = enc.encode(': keepalive\n\n');
    heartbeat = setInterval(() => send(keepalive), 20_000);
  }, (err) => {
    if (stopped) return;
    logger.warn({ err }, 'admin event stream unavailable');
    send(sse('unavailable', null));
    if (!stopped) close();
  });
  return stop;
}

function streamResponse(source: FeedSource): Response {
  let stop: () => void;
  const stream = new ReadableStream({
    start(controller) {
      stop = startStream(controller, source);
    },
    cancel() {
      stop();
    }
  });
  return new Response(stream, {
    headers: {
      'Content-Type': 'text/event-stream',
      'Cache-Control': 'no-store',
      Connection: 'keep-alive'
    }
  });
}

export const GET: RequestHandler = async ({ locals }) => {
  if (!(await requireAdmin(locals.session))) throw error(403, 'forbidden');
  return streamResponse(() => statusHistory(STATUS_PREFIX));
};
