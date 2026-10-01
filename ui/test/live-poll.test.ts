// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, describe, expect, test } from 'bun:test';

import { livePoll } from '../lib/live-poll';
import { snapshotGlobals } from './dom-fakes';

type Timer = { at: number; ms: number; fn: () => void };

function clock() {
  let now = 0;
  const queue: Timer[] = [];
  const realTimeout = globalThis.setTimeout;
  const realClear = globalThis.clearTimeout;
  globalThis.setTimeout = ((fn: () => void, ms: number) => {
    const entry = { at: now + ms, ms, fn };
    queue.push(entry);
    return entry;
  }) as unknown as typeof setTimeout;
  globalThis.clearTimeout = ((handle: unknown) => {
    const i = queue.indexOf(handle as Timer);
    if (i >= 0) queue.splice(i, 1);
  }) as unknown as typeof clearTimeout;
  return {
    now: () => now,
    queue,
    async settle() {
      for (let i = 0; i < 4; i += 1) await Promise.resolve();
    },
    async fire() {
      const due = queue.shift();
      if (due) now = due.at;
      due?.fn();
      await this.settle();
    },
    restore() {
      globalThis.setTimeout = realTimeout;
      globalThis.clearTimeout = realClear;
    },
  };
}

function page(hidden = false) {
  const listeners = new Set<() => void>();
  const doc = {
    hidden,
    addEventListener: (_: string, fn: () => void) => listeners.add(fn),
    removeEventListener: (_: string, fn: () => void) => listeners.delete(fn),
  };
  (globalThis as { document?: unknown }).document = doc;
  return {
    listeners,
    set(value: boolean) {
      doc.hidden = value;
      for (const fn of [...listeners]) fn();
    },
  };
}

const POLL_ENDINGS = [
  {
    name: 'stops on the first settled tick and reports done once',
    settledAt: 2,
    firstDelayMs: 500,
    timeoutMs: 30_000,
    fires: 2,
    expected: [2, 1, 0],
  },
  {
    name: 'gives up at the deadline even while the backend never settles',
    settledAt: Number.POSITIVE_INFINITY,
    firstDelayMs: 0,
    timeoutMs: 2000,
    fires: 3,
    expected: [3, 1, 0],
  },
];

describe('livePoll', () => {
  afterEach(snapshotGlobals(['document']));

  for (const { name, settledAt, firstDelayMs, timeoutMs, fires, expected } of POLL_ENDINGS) {
    test(name, async () => {
      const c = clock();
      let calls = 0;
      let done = 0;
      livePoll(async () => (calls += 1) >= settledAt, {
        firstDelayMs,
        delayMs: () => 1000,
        timeoutMs,
        onDone: () => (done += 1),
        now: c.now,
      });
      await c.fire();
      expect(c.queue.map((t) => t.ms)).toEqual([1000]);
      for (let fired = 1; fired < fires; fired += 1) await c.fire();
      c.restore();
      expect([calls, done, c.queue.length]).toEqual(expected);
    });
  }

  test('schedules nothing more once stopped mid-flight', async () => {
    const c = clock();
    let release: () => void = () => {};
    let done = 0;
    const stop = livePoll(
      () => new Promise<boolean>((resolve) => (release = () => resolve(false))),
      { firstDelayMs: 100, delayMs: () => 100, timeoutMs: 30_000, onDone: () => (done += 1), now: c.now },
    );
    await c.fire();
    stop();
    release();
    await c.settle();
    stop();
    c.restore();
    expect([done, c.queue.length]).toEqual([1, 0]);
  });

  test('a hidden page waits hiddenDelayMs between ticks', async () => {
    const c = clock();
    const vis = page(true);
    const stop = livePoll(async () => false, {
      firstDelayMs: 5000,
      delayMs: () => 5000,
      timeoutMs: Number.POSITIVE_INFINITY,
      hiddenDelayMs: 15_000,
      now: c.now,
    });
    await c.fire();
    expect(c.queue.map((t) => t.ms)).toEqual([15_000]);
    vis.set(false);
    await c.fire();
    expect(c.queue.map((t) => t.ms)).toEqual([5000]);
    stop();
    c.restore();
  });

  test('coming back to the page ticks at once and restarts the wait', async () => {
    const c = clock();
    const vis = page(true);
    let calls = 0;
    const stop = livePoll(async () => (calls += 1) < 0, {
      firstDelayMs: 5000,
      delayMs: () => 5000,
      timeoutMs: Number.POSITIVE_INFINITY,
      refreshOnVisible: true,
      now: c.now,
    });
    vis.set(true);
    expect(calls).toBe(0);
    vis.set(false);
    await c.settle();
    expect(calls).toBe(1);
    expect(c.queue).toHaveLength(1);
    stop();
    c.restore();
    expect(vis.listeners.size).toBe(0);
  });

  test('a visibility refresh never overlaps a tick already in flight', async () => {
    const c = clock();
    const vis = page(false);
    let calls = 0;
    let release: () => void = () => {};
    const stop = livePoll(
      () => {
        calls += 1;
        return new Promise<boolean>((resolve) => (release = () => resolve(false)));
      },
      { firstDelayMs: 100, delayMs: () => 100, timeoutMs: Number.POSITIVE_INFINITY, refreshOnVisible: true, now: c.now },
    );
    await c.fire();
    vis.set(false);
    expect(calls).toBe(1);
    release();
    await c.settle();
    expect(c.queue).toHaveLength(1);
    stop();
    c.restore();
  });

  test('without refreshOnVisible the page is never watched', () => {
    const c = clock();
    const vis = page(false);
    const stop = livePoll(async () => true, { firstDelayMs: 1, delayMs: () => 1, timeoutMs: 1, now: c.now });
    expect(vis.listeners.size).toBe(0);
    stop();
    c.restore();
  });
});
