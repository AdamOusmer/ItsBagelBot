// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { randomUUID } from 'node:crypto';
import type { RequestHandler } from './$types';
import { error } from '@sveltejs/kit';
import { dev } from '$app/env';
import { logger } from '@bagel/kit/server/logger';
import { requireAdmin } from '#lib/server/access';
import { statusHistory } from '#lib/server/feed';
import { FeedHistory, type FeedRecord } from '#lib/server/feed-history';
import { STATUS_PREFIX } from '#lib/server/services';

const enc = new TextEncoder();
const processId = randomUUID();
const DEMO = dev && process.env.DEMO === '1';
const DEMO_INTERVAL_MS = 3_000;
const DEMO_HISTORY_SIZE = 40;
let demoHistory: FeedHistory | undefined;

type DemoEvent = (sequence: number, prefix: string, at: string) => FeedRecord;
type FeedSource = () => Promise<FeedHistory>;

function demoStatusHistory(makeEvent: DemoEvent): FeedHistory {
  if (demoHistory) return demoHistory;
  const history = new FeedHistory();
  const now = Date.now();
  let sequence = 0;
  for (; sequence < DEMO_HISTORY_SIZE; sequence++) {
    const at = new Date(now - (DEMO_HISTORY_SIZE - sequence) * DEMO_INTERVAL_MS).toISOString();
    history.add(makeEvent(sequence, STATUS_PREFIX, at));
  }
  setInterval(() => {
    history.add(makeEvent(sequence++, STATUS_PREFIX, new Date().toISOString()));
  }, DEMO_INTERVAL_MS).unref();
  demoHistory = history;
  return history;
}

function sse(event: 'history' | 'feed', data: unknown): Uint8Array {
  const id = event === 'history' ? `id: ${processId}\n` : '';
  return enc.encode(`${id}event: ${event}\ndata: ${JSON.stringify(data)}\n\n`);
}

function startStream(controller: ReadableStreamDefaultController, source: FeedSource): () => void {
  controller.enqueue(enc.encode(': connected\n\n'));
  let stopped = false;
  let unsubscribe: (() => void) | undefined;
  const heartbeat = setInterval(() => controller.enqueue(enc.encode(': keepalive\n\n')), 20_000);
  const stop = () => {
    stopped = true;
    clearInterval(heartbeat);
    unsubscribe?.();
  };
  const close = () => {
    stop();
    controller.close();
  };
  void source().then((history) => {
    if (stopped) return;
    unsubscribe = history.listen({
      history: (events) => controller.enqueue(sse('history', events)),
      feed: (event) => controller.enqueue(sse('feed', event)),
      close
    });
  }, (err) => {
    if (stopped) return;
    logger.warn({ err }, 'admin event stream unavailable');
    controller.enqueue(sse('history', []));
    close();
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

  if (DEMO) {
    const { demoFeedEvent } = await import('#lib/server/demo-data');
    return streamResponse(async () => demoStatusHistory(demoFeedEvent));
  }
  return streamResponse(() => statusHistory(STATUS_PREFIX));
};
