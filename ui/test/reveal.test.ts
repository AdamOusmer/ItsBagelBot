// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, expect, test } from "bun:test";

type Stub = {
  classes: Set<string>;
  dataset: Record<string, string | undefined>;
  classList: { add(name: string): void };
  matches(selector: string): boolean;
  getBoundingClientRect(): { top: number; bottom: number };
};

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

const root = (children: Stub[]) => ({ querySelectorAll: () => children }) as unknown as ParentNode;

(globalThis as { HTMLElement?: unknown }).HTMLElement = class {};

let observed: Stub[];
let callbacks: ((entries: unknown[], observer: unknown) => void)[];

beforeEach(() => {
  observed = [];
  callbacks = [];
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

const { observeReveal } = await import("../lib/reveal");

test("below-fold elements are observed, above-fold ones reveal immediately", () => {
  const above = el(200);
  const below = el(4000);
  observeReveal(root([above, below]));

  expect(above.classes.has("is-revealed")).toBe(true);
  expect(below.classes.has("is-revealed")).toBe(false);
  expect(observed).toEqual([below]);
});

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

test("dispose releases the ready flag so a re-scan re-wires", () => {
  const below = el(4000);
  const dispose = observeReveal(root([below]));
  dispose();

  expect(below.dataset.revealReady).toBeUndefined();
  expect(observed).toEqual([]);

  observeReveal(root([below]));
  expect(observed).toEqual([below]);
});
