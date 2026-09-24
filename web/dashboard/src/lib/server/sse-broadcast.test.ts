// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, describe, expect, jest, test } from 'bun:test';
import { broadcastResponse, createBroadcast, type Sink } from './sse-broadcast';

const TICK = 1000;
const dec = new TextDecoder();

async function flush(): Promise<void> {
  for (let i = 0; i < 10; i++) await Promise.resolve();
}

function collector(): { sink: Sink; frames: string[] } {
  const frames: string[] = [];
  return { sink: (chunk) => frames.push(dec.decode(chunk)), frames };
}

function counting(label = 'f') {
  let calls = 0;
  const frame = async (key: string) => `${label}:${key}:${++calls}`;
  return { frame, calls: () => calls };
}

function deferred() {
  let resolve!: (v: string) => void;
  const promise = new Promise<string>((r) => (resolve = r));
  return { promise, resolve };
}

describe('createBroadcast', () => {
  beforeEach(() => jest.useFakeTimers());
  afterEach(() => jest.useRealTimers());

  test('first subscriber gets a frame without waiting a tick', async () => {
    const f = counting();
    const b = createBroadcast(TICK, [f.frame]);
    const a = collector();
    b.subscribe('k', a.sink);
    await flush();
    expect(a.frames).toEqual(['f:k:1']);
  });

  test('many subscribers on one key share one produce per tick', async () => {
    const f = counting();
    const b = createBroadcast(TICK, [f.frame]);
    const subs = Array.from({ length: 50 }, collector);
    for (const s of subs) b.subscribe('k', s.sink);
    await flush();
    jest.advanceTimersByTime(TICK * 3);
    await flush();
    expect(f.calls()).toBe(4);
    for (const s of subs.slice(1)) expect(s.frames).toEqual(subs[0].frames);
  });

  test('late joiner replays the last frame instead of producing again', async () => {
    const f = counting();
    const b = createBroadcast(TICK, [f.frame]);
    b.subscribe('k', collector().sink);
    await flush();
    const late = collector();
    b.subscribe('k', late.sink);
    expect(late.frames).toEqual(['f:k:1']);
    expect(f.calls()).toBe(1);
  });

  test('replays every frame kind the channel has produced', async () => {
    const b = createBroadcast(TICK, [async () => 'a', async () => 'b']);
    b.subscribe('k', collector().sink);
    await flush();
    const late = collector();
    b.subscribe('k', late.sink);
    expect(late.frames).toEqual(['a', 'b']);
  });

  test('last unsubscribe stops the ticker', async () => {
    const f = counting();
    const b = createBroadcast(TICK, [f.frame]);
    const off1 = b.subscribe('k', collector().sink);
    const off2 = b.subscribe('k', collector().sink);
    await flush();
    off1();
    jest.advanceTimersByTime(TICK);
    await flush();
    expect(f.calls()).toBe(2);
    off2();
    jest.advanceTimersByTime(TICK * 10);
    await flush();
    expect(f.calls()).toBe(2);
  });

  test('resubscribing after the channel emptied starts fresh with no stale replay', async () => {
    const f = counting();
    const b = createBroadcast(TICK, [f.frame]);
    b.subscribe('k', collector().sink)();
    await flush();
    const again = collector();
    b.subscribe('k', again.sink);
    expect(again.frames).toEqual([]);
    await flush();
    expect(again.frames).toEqual(['f:k:2']);
  });

  test('double unsubscribe does not stop a channel others still watch', async () => {
    const f = counting();
    const b = createBroadcast(TICK, [f.frame]);
    const off = b.subscribe('k', collector().sink);
    const stay = collector();
    b.subscribe('k', stay.sink);
    await flush();
    off();
    off();
    jest.advanceTimersByTime(TICK);
    await flush();
    expect(stay.frames).toEqual(['f:k:1', 'f:k:2']);
  });

  test('keys are isolated channels', async () => {
    const f = counting();
    const b = createBroadcast(TICK, [f.frame]);
    const x = collector();
    const y = collector();
    b.subscribe('x', x.sink);
    b.subscribe('y', y.sink);
    await flush();
    expect(x.frames).toEqual(['f:x:1']);
    expect(y.frames).toEqual(['f:y:2']);
  });

  test('a slow frame is not stacked by later ticks', async () => {
    let calls = 0;
    const slow = deferred();
    const b = createBroadcast(TICK, [
      () => {
        calls++;
        return slow.promise;
      }
    ]);
    const a = collector();
    b.subscribe('k', a.sink);
    jest.advanceTimersByTime(TICK * 5);
    await flush();
    expect(calls).toBe(1);
    slow.resolve('done');
    await flush();
    expect(a.frames).toEqual(['done']);
    jest.advanceTimersByTime(TICK);
    await flush();
    expect(calls).toBe(2);
  });

  test('a slow frame kind does not hold back the others', async () => {
    const slow = deferred();
    const b = createBroadcast(TICK, [async () => 'fast', () => slow.promise]);
    const a = collector();
    b.subscribe('k', a.sink);
    await flush();
    expect(a.frames).toEqual(['fast']);
    slow.resolve('slow');
    await flush();
    expect(a.frames).toEqual(['fast', 'slow']);
  });

  test('a failing frame is skipped and the next tick retries', async () => {
    let n = 0;
    const b = createBroadcast(TICK, [
      async () => {
        if (++n === 1) throw new Error('boom');
        return 'ok';
      }
    ]);
    const a = collector();
    b.subscribe('k', a.sink);
    await flush();
    expect(a.frames).toEqual([]);
    jest.advanceTimersByTime(TICK);
    await flush();
    expect(a.frames).toEqual(['ok']);
  });

  test('a synchronously throwing frame does not escape', async () => {
    const b = createBroadcast<string>(TICK, [
      () => {
        throw new Error('sync');
      }
    ]);
    expect(() => b.subscribe('k', collector().sink)).not.toThrow();
    await flush();
  });

  test('a throwing sink does not starve the other sinks', async () => {
    const b = createBroadcast(TICK, [async () => 'x']);
    b.subscribe('k', () => {
      throw new Error('dead socket');
    });
    const ok = collector();
    b.subscribe('k', ok.sink);
    await flush();
    expect(ok.frames).toEqual(['x']);
  });
});

describe('broadcastResponse', () => {
  test('streams the connect comment then broadcast frames', async () => {
    const b = createBroadcast(60_000, [async () => 'data: 1\n\n']);
    const res = broadcastResponse(new Request('http://x/s'), b, 'k');
    expect(res.headers.get('content-type')).toBe('text/event-stream');
    const reader = res.body!.getReader();
    expect(dec.decode((await reader.read()).value)).toBe(': connected\n\n');
    expect(dec.decode((await reader.read()).value)).toBe('data: 1\n\n');
    await reader.cancel();
  });

  const disconnects: [string, (ac: AbortController, res: Response) => Promise<unknown> | void][] = [
    ['client abort', (ac) => ac.abort()],
    ['stream cancel', (_ac, res) => res.body!.cancel()]
  ];

  test.each(disconnects)('%s unsubscribes so an emptied channel stops producing', async (_name, disconnect) => {
    jest.useFakeTimers();
    try {
      const f = counting();
      const b = createBroadcast(TICK, [f.frame]);
      const ac = new AbortController();
      const res = broadcastResponse(new Request('http://x/s', { signal: ac.signal }), b, 'k');
      await flush();
      await disconnect(ac, res);
      jest.advanceTimersByTime(TICK * 5);
      await flush();
      expect(f.calls()).toBe(1);
    } finally {
      jest.useRealTimers();
    }
  });

  test('an already aborted request never holds a subscription', async () => {
    const f = counting();
    const b = createBroadcast(60_000, [f.frame]);
    const ac = new AbortController();
    ac.abort();
    broadcastResponse(new Request('http://x/s', { signal: ac.signal }), b, 'k');
    await flush();
    const probe = collector();
    b.subscribe('k', probe.sink);
    expect(probe.frames).toEqual([]);
  });
});
