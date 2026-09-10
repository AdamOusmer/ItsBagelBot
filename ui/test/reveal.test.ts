// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// observeReveal's WIRING, not its animation. What is worth pinning here is the
// lifecycle around the `data-reveal-ready` flag, because getting it wrong is
// invisible: the page still loads, the console stays clean, the observer still
// exists, and nothing ever reveals. That shipped once — the marketing host
// disposes and re-scans on `astro:page-load`, which also fires on a cold load
// right after the module's own first call, and with the flag left set the
// second scan found 0 of 50 elements and observed nothing.
//
// Bun has no DOM. Rather than pull in happy-dom for one module, the handful of
// DOM surfaces reveal.ts actually touches are stubbed by hand: classList.add,
// dataset, getBoundingClientRect, matches, querySelectorAll, and the
// IntersectionObserver constructor. Everything else about an Element is
// irrelevant to this file, and a stub that only implements what is used fails
// loudly (undefined is not a function) the day reveal.ts starts using more.

import { afterEach, beforeEach, expect, test } from "bun:test";

type Stub = {
  classes: Set<string>;
  dataset: Record<string, string | undefined>;
  classList: { add(name: string): void };
  matches(selector: string): boolean;
  getBoundingClientRect(): { top: number; bottom: number };
};

/** One `[data-reveal]` element. `top` is its viewport offset in px. */
function el(top: number): Stub {
  const classes = new Set<string>();
  return {
    classes,
    dataset: {},
    classList: { add: (name: string) => void classes.add(name) },
    matches: (selector: string) => selector === "[data-reveal]",
    getBoundingClientRect: () => ({ top, bottom: top + 100 }),
  };
}

/** A `ParentNode` that yields `children` from querySelectorAll. */
const root = (children: Stub[]) => ({ querySelectorAll: () => children }) as unknown as ParentNode;

// `targetsIn` asks `root instanceof HTMLElement` to decide whether the root
// itself counts as a target. Installed once, for the whole file: the stub roots
// here are plain objects, so the answer is always false and the "root carries
// data-reveal" path belongs to the Svelte action, not to a document scan.
(globalThis as { HTMLElement?: unknown }).HTMLElement = class {};

/** Every element handed to an observer that has not been disconnected. */
let observed: Stub[];
/** The intersection callbacks of the observers created so far. */
let callbacks: ((entries: unknown[], observer: unknown) => void)[];

beforeEach(() => {
  observed = [];
  callbacks = [];
  // window.innerHeight: reveal.ts calls an element on-screen when its top is
  // above the fold, and every case below is expressed relative to this.
  (globalThis as { window?: unknown }).window = { innerHeight: 1000 };
  (globalThis as { IntersectionObserver?: unknown }).IntersectionObserver = class {
    #live = true;
    constructor(cb: (entries: unknown[], observer: unknown) => void) {
      callbacks.push(cb);
    }
    observe(target: Stub) {
      if (this.#live) observed.push(target);
    }
    disconnect() {
      this.#live = false;
      observed = observed.filter((t) => !this.#owns(t));
    }
    #owns(target: Stub) {
      return observed.includes(target);
    }
  };
});

afterEach(() => {
  delete (globalThis as { window?: unknown }).window;
  delete (globalThis as { IntersectionObserver?: unknown }).IntersectionObserver;
});

// Dynamic, so the stubs above are in place before the module reads its globals.
const { observeReveal } = await import("../lib/reveal");

test("below-fold elements are observed, above-fold ones reveal immediately", () => {
  const above = el(200);
  const below = el(4000);
  observeReveal(root([above, below]));

  expect(above.classes.has("is-revealed")).toBe(true);
  expect(below.classes.has("is-revealed")).toBe(false);
  expect(observed).toEqual([below]);
});

/** Deliver `targets` to the latest observer as intersecting. Returns what it unobserved. */
function intersect(...targets: Stub[]): Stub[] {
  const unobserved: Stub[] = [];
  callbacks[0]!(
    targets.map((target) => ({ isIntersecting: true, target })),
    { unobserve: (t: Stub) => void unobserved.push(t) },
  );
  return unobserved;
}

test("an intersecting entry reveals; only a non-repeating one is unobserved", () => {
  const once = el(4000);
  const repeat = el(4000);
  repeat.dataset.revealRepeat = "true";
  observeReveal(root([once, repeat]));

  expect(intersect(once, repeat)).toEqual([once]);
  expect(once.classes.has("is-revealed")).toBe(true);
  expect(repeat.classes.has("is-revealed")).toBe(true);
});

test("a second scan over the same elements does not double-observe", () => {
  const below = el(4000);
  observeReveal(root([below]));
  observeReveal(root([below]));

  expect(observed).toEqual([below]);
});

// The regression. Everything above passes with a teardown that only calls
// observer.disconnect(); this is the case that does not.
test("dispose releases the ready flag so a re-scan re-wires", () => {
  const below = el(4000);
  const dispose = observeReveal(root([below]));
  dispose();

  expect(below.dataset.revealReady).toBeUndefined();
  expect(observed).toEqual([]);

  observeReveal(root([below]));
  expect(observed).toEqual([below]);
});
