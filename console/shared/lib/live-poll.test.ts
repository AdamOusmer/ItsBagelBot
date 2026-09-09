// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, it } from 'bun:test';
import { livePoll } from './live-poll';

// A hand-driven clock and timer queue: bun's fake timers do not advance the
// microtasks between two ticks, and every interesting property of this loop
// (does a stop mid-fetch schedule another tick, does the deadline hold) only
// shows up across an await. `flush` runs the queued timer AND lets its promise
// chain settle before returning.
function harness() {
  let now = 0;
  const queue: { at: number; fn: () => void }[] = [];
  const timer = ((fn: () => void, ms: number) => {
    const entry = { at: now + ms, fn };
    queue.push(entry);
    return entry as unknown as ReturnType<typeof setTimeout>;
  }) as unknown as typeof setTimeout;
  const clear = ((h: unknown) => {
    const i = queue.indexOf(h as { at: number; fn: () => void });
    if (i >= 0) queue.splice(i, 1);
  }) as unknown as typeof clearTimeout;

  return {
    now: () => now,
    install() {
      globalThis.setTimeout = timer;
      globalThis.clearTimeout = clear;
    },
    async flush(advanceMs: number) {
      now += advanceMs;
      const due = queue.shift();
      due?.fn();
      // Two turns: one for the tick's own await, one for the loop's.
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    },
    pending: () => queue.length
  };
}

describe('livePoll', () => {
  const realTimeout = globalThis.setTimeout;
  const realClear = globalThis.clearTimeout;

  const restore = () => {
    globalThis.setTimeout = realTimeout;
    globalThis.clearTimeout = realClear;
  };

  it('stops on the first settled tick and reports done once', async () => {
    const h = harness();
    h.install();
    let calls = 0;
    let done = 0;
    livePoll(
      async () => {
        calls += 1;
        return calls === 2;
      },
      { firstDelayMs: 500, delayMs: () => 1000, timeoutMs: 30_000, onDone: () => (done += 1), now: h.now }
    );

    expect(calls).toBe(0); // nothing runs before the first delay
    await h.flush(500);
    expect(calls).toBe(1);
    expect(h.pending()).toBe(1);
    await h.flush(1000);
    restore();

    expect(calls).toBe(2);
    expect(done).toBe(1);
    expect(h.pending()).toBe(0);
  });

  it('gives up at the deadline even while the backend never settles', async () => {
    const h = harness();
    h.install();
    let calls = 0;
    let done = 0;
    livePoll(
      async () => {
        calls += 1;
        return false;
      },
      { firstDelayMs: 0, delayMs: () => 1000, timeoutMs: 2000, onDone: () => (done += 1), now: h.now }
    );

    await h.flush(0);
    await h.flush(1000);
    expect(calls).toBe(2);
    await h.flush(1000); // elapsed hits the 2000 ms ceiling
    restore();

    expect(calls).toBe(3);
    expect(done).toBe(1);
    expect(h.pending()).toBe(0);
  });

  it('schedules nothing more once stopped mid-flight', async () => {
    const h = harness();
    h.install();
    let calls = 0;
    let done = 0;
    let release: (() => void) | null = null;
    const stop = livePoll(
      async () => {
        calls += 1;
        await new Promise<void>((r) => (release = r));
        return false;
      },
      { firstDelayMs: 100, delayMs: () => 100, timeoutMs: 30_000, onDone: () => (done += 1), now: h.now }
    );

    await h.flush(100);
    expect(calls).toBe(1);
    stop();
    expect(done).toBe(1);
    release?.();
    await Promise.resolve();
    await Promise.resolve();
    restore();

    // The tick resolved after the stop: it must not have armed another timer.
    expect(h.pending()).toBe(0);
    expect(calls).toBe(1);
    stop(); // idempotent
    expect(done).toBe(1);
  });
});
