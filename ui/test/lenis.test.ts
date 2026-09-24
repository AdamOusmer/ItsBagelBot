// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

const { createPaneScroll, createSmoothScroll, getSmoothScroll } = await import("../lib/lenis");

type SeenOptions = Record<string, unknown> & {
  prevent: (node: unknown) => boolean;
  virtualScroll: (data: unknown) => boolean;
};
const seen = () => lastOptions as SeenOptions;
const wheel = (deltaY: number) => ({ deltaX: 0, deltaY, event: { type: "wheel" } });

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
  reduced = false;
});

type Handle = NonNullable<ReturnType<typeof createSmoothScroll>>;

function start(): Handle {
  const handle = createSmoothScroll();
  expect(handle).not.toBeNull();
  return handle!;
}

function adopt(): { first: Handle; second: Handle } {
  const first = start();
  const second = start();
  expect(second.lenis).toBe(first.lenis);
  expect(constructed).toBe(1);
  expect(subscriptions).toBe(1);
  return { first, second };
}

describe("createSmoothScroll", () => {
  test("constructs one scroller and publishes it on window.__lenis", () => {
    const handle = start();

    expect(constructed).toBe(1);
    expect(subscriptions).toBe(1);
    expect(getSmoothScroll()).toBe(handle.lenis);
    expect((globalThis as unknown as LenisWindow).__lenis).toBe(handle.lenis);
  });

  test("fixes lerp, smoothWheel and syncTouch, and wraps the caller's knobs in the nested-scroll gate", () => {
    const prevent = (node: HTMLElement) => node.id === "docs-sidebar";
    const virtualScroll = () => false;
    createSmoothScroll({ prevent, virtualScroll });

    expect(lastOptions).toMatchObject({ lerp: 0.1, smoothWheel: true, syncTouch: false });
    expect(lastOptions).not.toHaveProperty("allowNestedScroll");

    expect(seen().prevent).not.toBe(prevent);
    expect(seen().prevent({ id: "docs-sidebar" })).toBe(true);
    expect(seen().virtualScroll(wheel(1))).toBe(false);
  });

  test("a second call returns the LIVE instance and constructs nothing", () => {
    adopt();
  });

  test("the second caller's destroy is a no-op, so it cannot tear down the first", () => {
    const { first, second } = adopt();

    second.destroy();

    expect(destroyed).toBe(0);
    expect(subscriptions).toBe(1);
    expect(getSmoothScroll()).toBe(first.lenis);
  });

  test("destroy unsubscribes, drops the global and destroys the instance", () => {
    start().destroy();

    expect(destroyed).toBe(1);
    expect(subscriptions).toBe(0);
    expect(getSmoothScroll()).toBeUndefined();

    start();
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
    const first = start();
    reduced = true;

    expect(createSmoothScroll()!.lenis).toBe(first.lenis);
    expect(constructed).toBe(1);
  });
});

describe("createPaneScroll", () => {
  const pane = {} as HTMLElement;

  test("never touches window.__lenis, so the page scroller stays the one overlays stop", () => {
    const page = createSmoothScroll();
    const handle = createPaneScroll(pane);

    expect(handle!.lenis).not.toBe(page!.lenis);
    expect(getSmoothScroll()).toBe(page!.lenis);
    expect(constructed).toBe(2);
  });

  test("scrolls the pane itself and declines the wheel while it has nothing to scroll", () => {
    const limit = createPaneScroll(pane)!.lenis as unknown as { limit: number };
    const { wrapper, content, naiveDimensions, virtualScroll } = seen();
    const verdictAt = (max: number) => {
      limit.limit = max;
      return virtualScroll(wheel(40));
    };

    expect([wrapper, content, naiveDimensions]).toEqual([pane, pane, true]);
    expect([0, 120].map(verdictAt)).toEqual([false, true]);
  });

  test("destroy unsubscribes and destroys only its own instance", () => {
    const page = createSmoothScroll();
    createPaneScroll(pane)!.destroy();

    expect(destroyed).toBe(1);
    expect(subscriptions).toBe(1);
    expect(getSmoothScroll()).toBe(page!.lenis);
  });

  test("returns null under reduced motion, and constructs nothing", () => {
    reduced = true;

    expect(createPaneScroll(pane)).toBeNull();
    expect(constructed).toBe(0);
  });
});
