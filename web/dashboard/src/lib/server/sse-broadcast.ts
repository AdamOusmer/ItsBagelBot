// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

export type Frame<K> = (key: K) => Promise<string>;
export type Sink = (chunk: Uint8Array) => void;

export interface Broadcast<K> {
  subscribe(key: K, sink: Sink): () => void;
}

interface Channel {
  sinks: Set<Sink>;
  last: (Uint8Array | undefined)[];
  busy: boolean[];
  timer?: ReturnType<typeof setInterval>;
}

const enc = new TextEncoder();
const CONNECTED = enc.encode(': connected\n\n');

function deliver(sink: Sink, chunk: Uint8Array): void {
  // One broken connection must never starve the others.
  try {
    sink(chunk);
  } catch {}
}

export function createBroadcast<K>(intervalMs: number, frames: Frame<K>[]): Broadcast<K> {
  const channels = new Map<K, Channel>();

  async function produce(key: K, ch: Channel, i: number): Promise<void> {
    if (ch.busy[i]) return;
    ch.busy[i] = true;
    try {
      const chunk = enc.encode(await frames[i](key));
      ch.last[i] = chunk;
      for (const sink of ch.sinks) deliver(sink, chunk);
    } catch {
    } finally {
      ch.busy[i] = false;
    }
  }

  function tick(key: K, ch: Channel): void {
    for (let i = 0; i < frames.length; i++) void produce(key, ch, i);
  }

  function subscribe(key: K, sink: Sink): () => void {
    let ch = channels.get(key);
    const fresh = !ch;
    if (!ch) {
      const created: Channel = { sinks: new Set(), last: [], busy: [] };
      created.timer = setInterval(() => tick(key, created), intervalMs);
      channels.set(key, created);
      ch = created;
    } else {
      for (const chunk of ch.last) if (chunk) deliver(sink, chunk);
    }
    ch.sinks.add(sink);
    if (fresh) tick(key, ch);

    const owned = ch;
    return () => {
      if (!owned.sinks.delete(sink) || owned.sinks.size > 0) return;
      clearInterval(owned.timer);
      if (channels.get(key) === owned) channels.delete(key);
    };
  }

  return { subscribe };
}

export function broadcastResponse<K>(request: Request, broadcast: Broadcast<K>, key: K): Response {
  let cleanup = () => {};

  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      let closed = false;
      let unsubscribe = () => {};
      cleanup = () => {
        if (closed) return;
        closed = true;
        unsubscribe();
      };
      const send: Sink = (chunk) => {
        if (closed) return;
        try {
          controller.enqueue(chunk);
        } catch {
          cleanup();
        }
      };

      send(CONNECTED);
      const off = broadcast.subscribe(key, send);
      if (closed) off();
      else unsubscribe = off;

      if (request.signal.aborted) cleanup();
      else request.signal.addEventListener('abort', cleanup);
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
}
