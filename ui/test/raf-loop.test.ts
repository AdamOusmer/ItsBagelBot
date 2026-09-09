// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The scheduler is the one module in lib/ whose correctness is invisible on
// screen. A loop that never settles looks identical to one that does; the
// difference is a laptop fan and a battery graph. A loop that settles and
// cannot be woken looks identical to a broken effect, and gets debugged in the
// effect, for hours. So it gets the unit test that the drawing code does not
// need.
//
// Bun has no DOM, and lib/raf-loop.ts reads `requestAnimationFrame` and
// `document` as bare globals resolved at call time. That is what makes this
// testable at all: the stubs are installed on globalThis BEFORE the module is
// imported (hence the top-level await on a dynamic import, rather than a static
// import that would hoist above the stubs), so the module's own
// visibilitychange registration lands on the fake document too.
//
// The scheduler is a module-level singleton and bun's module registry is per
// process, so there is exactly ONE instance for the whole file. Every test
// therefore registers through `track()` and `afterEach` unsubscribes: a
// subscriber left behind would be woken by the next test's argument-less
// `wake()` and count a tick that test never asked for.
//
// `frames()` drains the queue one frame at a time rather than looping until it
// is empty: a subscriber that never settles would make "until empty" hang
// forever, and a CI hang is a far worse failure report than a wrong count.

import { afterEach, beforeEach, describe, expect, test } from "bun:test";

// Type-only: erased at compile time, so it does not pull the module in above
// the global stubs the way a value import would.
import type { Tick } from "../lib/raf-loop";

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

const loop = await import("../lib/raf-loop");

/** Run every callback queued so far, exactly once, `count` times over. */
function frames(count = 1): void {
  for (let i = 0; i < count; i += 1) {
    const pending = queue;
    queue = [];
    now += 16;
    for (const callback of pending) callback(now);
  }
}

/** Flip `document.hidden` and fire the event the module listens for. */
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
    expect(loop.isRunning()).toBe(true); // the second one still wants frames

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
