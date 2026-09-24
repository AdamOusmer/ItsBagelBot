// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, afterEach, beforeEach, describe, expect, test } from "bun:test";
import { copyFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

import type { Tick } from "../lib/raf-loop";

const savedGlobals = new Map(
  ['document', 'requestAnimationFrame', 'cancelAnimationFrame'].map((key) =>
    [key, Object.getOwnPropertyDescriptor(globalThis, key)] as const),
);
afterAll(() => {
  for (const [key, descriptor] of savedGlobals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else Reflect.deleteProperty(globalThis, key);
  }
});

let queue: Array<(time: number) => void> = [];
let now = 0;
let hidden = false;
const visibilityListeners: Array<() => void> = [];

let nextHandle = 1;
const live = new Set<number>();

globalThis.requestAnimationFrame = ((callback: FrameRequestCallback) => {
  const handle = nextHandle++;
  live.add(handle);
  queue.push((time) => {
    if (live.delete(handle)) callback(time);
  });
  return handle;
}) as typeof requestAnimationFrame;

globalThis.cancelAnimationFrame = ((handle: number) => {
  live.delete(handle);
}) as typeof cancelAnimationFrame;

(globalThis as { document?: unknown }).document = {
  get hidden() {
    return hidden;
  },
  addEventListener(type: string, listener: () => void) {
    if (type === "visibilitychange") visibilityListeners.push(listener);
  },
  removeEventListener() {},
};

const isolatedDir = mkdtempSync(join(tmpdir(), 'bagel-raf-test-'));
afterAll(() => rmSync(isolatedDir, { recursive: true, force: true }));
const schedulerPath = join(isolatedDir, 'raf-loop.ts');
copyFileSync(new URL('../lib/raf-loop.ts', import.meta.url), schedulerPath);
const loop = await import(schedulerPath) as typeof import('../lib/raf-loop');

function frames(count = 1): void {
  for (let i = 0; i < count; i += 1) {
    const pending = queue;
    queue = [];
    now += 16;
    for (const callback of pending) callback(now);
  }
}

function setHidden(value: boolean): void {
  hidden = value;
  for (const listener of visibilityListeners) listener();
}

const cleanups: Array<() => void> = [];

function track(tick: Tick): () => void {
  const unsubscribe = loop.subscribe(tick);
  cleanups.push(unsubscribe);
  return unsubscribe;
}

beforeEach(() => {
  queue = [];
  hidden = false;
});

afterEach(() => {
  for (const cleanup of cleanups.splice(0)) cleanup();
  queue = [];
});

describe("subscribe", () => {
  test("ticks every frame until the subscriber returns false", () => {
    let ticks = 0;
    track(() => {
      ticks += 1;
      return ticks < 3 ? undefined : false;
    });

    frames(5);

    expect(ticks).toBe(3);
    expect(loop.isRunning()).toBe(false);
  });

  test("settles only when the LAST subscriber settles", () => {
    track(() => false);
    let longRunning = 0;
    track(() => {
      longRunning += 1;
      return longRunning < 4 ? undefined : false;
    });

    frames(1);
    expect(loop.isRunning()).toBe(true);

    frames(5);
    expect(longRunning).toBe(4);
    expect(loop.isRunning()).toBe(false);
  });

  test("runs every subscriber inside one frame, in subscribe order", () => {
    const order: string[] = [];
    track(() => {
      order.push("a");
      return false;
    });
    track(() => {
      order.push("b");
      return false;
    });

    frames(1);

    expect(order).toEqual(["a", "b"]);
  });

  test("unsubscribe stops the ticks and cancels the pending frame", () => {
    let ticks = 0;
    const unsubscribe = track(() => {
      ticks += 1;
    });

    frames(1);
    expect(ticks).toBe(1);

    unsubscribe();
    expect(loop.isRunning()).toBe(false);

    frames(3);
    expect(ticks).toBe(1);
  });

  test("a subscriber may unsubscribe itself from inside its own tick", () => {
    let ticks = 0;
    let unsubscribe = () => {};
    unsubscribe = track(() => {
      ticks += 1;
      unsubscribe();
    });

    frames(3);

    expect(ticks).toBe(1);
  });
});

describe("wake", () => {
  test("restarts a settled subscriber", () => {
    let ticks = 0;
    track(() => {
      ticks += 1;
      return false;
    });

    frames(3);
    expect(ticks).toBe(1);

    loop.wake();
    frames(3);
    expect(ticks).toBe(2);
  });

  test("wakes only the named subscriber", () => {
    let a = 0;
    let b = 0;
    const tickA = () => {
      a += 1;
      return false;
    };
    track(tickA);
    track(() => {
      b += 1;
      return false;
    });

    frames(2);
    expect([a, b]).toEqual([1, 1]);

    loop.wake(tickA);
    frames(2);
    expect([a, b]).toEqual([2, 1]);
  });

  test("ignores a tick that is no longer subscribed", () => {
    let ticks = 0;
    const tick = () => {
      ticks += 1;
      return false;
    };
    const unsubscribe = track(tick);
    frames(1);
    unsubscribe();

    loop.wake(tick);

    expect(loop.isRunning()).toBe(false);
    frames(2);
    expect(ticks).toBe(1);
  });
});

describe("document.hidden", () => {
  test("stops scheduling while hidden and resumes when the tab returns", () => {
    let ticks = 0;
    track(() => {
      ticks += 1;
    });
    frames(1);
    expect(ticks).toBe(1);

    setHidden(true);
    expect(loop.isRunning()).toBe(false);
    frames(3);
    expect(ticks).toBe(1);

    setHidden(false);
    expect(loop.isRunning()).toBe(true);
    frames(1);
    expect(ticks).toBe(2);
  });

  test("subscribing while hidden queues no frame", () => {
    setHidden(true);

    let ticks = 0;
    track(() => {
      ticks += 1;
    });

    expect(loop.isRunning()).toBe(false);
    frames(2);
    expect(ticks).toBe(0);

    setHidden(false);
    frames(1);
    expect(ticks).toBe(1);
  });
});
