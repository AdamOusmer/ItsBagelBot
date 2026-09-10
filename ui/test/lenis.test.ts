// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// What this pins is the property that cannot be seen by looking at the page:
// that `createSmoothScroll` called twice produces ONE scroller.
//
// Two Lenis instances over the same document do not throw, do not warn, and do
// not look broken on a first scroll. They look like the page stuttering
// backwards under fast wheeling, because both are writing `scrollTop` from
// their own interpolation of the same target. That symptom gets debugged as a
// CSS or a compositor problem for a long time before anyone counts the
// instances — and it is reachable in one click on every surface here, because
// Astro's ClientRouter re-runs module scripts after a swap and SvelteKit
// re-runs `$effect` on navigation.
//
// The real `lenis` is stubbed rather than imported. Not for speed: the real one
// needs a document, a scrollable body and an event loop, none of which bun has,
// and none of which this test is about. `mock.module` must run before the
// module under test is imported, hence the top-level await on the dynamic
// import.

import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";

let constructed = 0;
let destroyed = 0;
let lastOptions: Record<string, unknown> | undefined;

class StubLenis {
  constructor(options: Record<string, unknown>) {
    constructed += 1;
    lastOptions = options;
  }
  raf(): void {}
  destroy(): void {
    destroyed += 1;
  }
}

mock.module("lenis", () => ({ default: StubLenis }));

let reduced = false;

// motion-query and raf-loop both read the environment at module load, so they
// are stubbed too: the point here is the instance bookkeeping, and a real
// scheduler would need a real requestAnimationFrame to prove nothing extra.
mock.module(new URL("../lib/motion-query.ts", import.meta.url).pathname, () => ({
  prefersReducedMotion: () => reduced,
  hasFinePointer: () => true,
  reduceMotion: { matches: false, addEventListener() {}, removeEventListener() {} },
  finePointer: { matches: true, addEventListener() {}, removeEventListener() {} },
}));

let subscriptions = 0;
mock.module(new URL("../lib/raf-loop.ts", import.meta.url).pathname, () => ({
  subscribe: () => {
    subscriptions += 1;
    return () => {
      subscriptions -= 1;
    };
  },
  wake: () => {},
  isRunning: () => false,
}));

type LenisWindow = { __lenis?: unknown };

(globalThis as { window?: unknown }).window = globalThis as unknown as Window;

const { createSmoothScroll, getSmoothScroll } = await import("../lib/lenis");

beforeEach(() => {
  constructed = 0;
  destroyed = 0;
  subscriptions = 0;
  reduced = false;
  lastOptions = undefined;
  delete (globalThis as unknown as LenisWindow).__lenis;
});

afterEach(() => {
  delete (globalThis as unknown as LenisWindow).__lenis;
  // Also reset here, not only in beforeEach. `mock.module` is process-wide and
  // permanent: this stub is motion-query for EVERY test file in the run, not
  // just this one, and bun does not promise which file runs first. A `reduced`
  // left true by the last test here is read by the next file's import of
  // prefersReducedMotion — which is how reveal.test.ts came to see reduced
  // motion it never asked for and take the "reveal everything, observe
  // nothing" branch in all five of its cases.
  reduced = false;
});

describe("createSmoothScroll", () => {
  test("constructs one scroller and publishes it on window.__lenis", () => {
    const handle = createSmoothScroll();

    expect(handle).not.toBeNull();
    expect(constructed).toBe(1);
    expect(subscriptions).toBe(1);
    expect(getSmoothScroll()).toBe(handle!.lenis);
    expect((globalThis as unknown as LenisWindow).__lenis).toBe(handle!.lenis);
  });

  test("fixes lerp, smoothWheel and syncTouch, and passes the caller's knobs through", () => {
    const prevent = () => true;
    createSmoothScroll({ prevent });

    expect(lastOptions).toMatchObject({
      lerp: 0.1,
      smoothWheel: true,
      syncTouch: false,
      prevent,
    });
  });

  test("a second call returns the LIVE instance and constructs nothing", () => {
    const first = createSmoothScroll();
    const second = createSmoothScroll();

    expect(constructed).toBe(1);
    expect(subscriptions).toBe(1);
    expect(second!.lenis).toBe(first!.lenis);
  });

  test("the second caller's destroy is a no-op, so it cannot tear down the first", () => {
    const first = createSmoothScroll();
    const second = createSmoothScroll();

    second!.destroy();

    expect(destroyed).toBe(0);
    expect(subscriptions).toBe(1);
    expect(getSmoothScroll()).toBe(first!.lenis);
  });

  test("destroy unsubscribes, drops the global and destroys the instance", () => {
    const handle = createSmoothScroll();
    handle!.destroy();

    expect(destroyed).toBe(1);
    expect(subscriptions).toBe(0);
    expect(getSmoothScroll()).toBeUndefined();

    // And the surface can start over afterwards.
    expect(createSmoothScroll()).not.toBeNull();
    expect(constructed).toBe(2);
  });

  test("returns null under reduced motion, and constructs nothing", () => {
    reduced = true;

    expect(createSmoothScroll()).toBeNull();
    expect(constructed).toBe(0);
    expect(subscriptions).toBe(0);
    expect(getSmoothScroll()).toBeUndefined();
  });

  test("adopts an already-running scroller even under reduced motion", () => {
    // The setting can flip mid-session. Adopting beats returning null: the
    // instance is real and something has to be able to stop it.
    const first = createSmoothScroll();
    reduced = true;

    expect(createSmoothScroll()!.lenis).toBe(first!.lenis);
    expect(constructed).toBe(1);
  });
});
