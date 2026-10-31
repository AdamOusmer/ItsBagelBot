// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, expect, test } from "bun:test";

type Stub = {
  classes: Set<string>;
  classList: {
    add(name: string): void;
    remove(name: string): void;
    contains(name: string): boolean;
  };
  matches(selector: string): boolean;
  querySelectorAll(selector: string): Stub[];
  getBoundingClientRect(): { top: number; bottom: number };
};

function el(top: number, nested: Stub[] = [], initial: string[] = ["reveal-in"]): Stub {
  const classes = new Set<string>(initial);
  return {
    classes,
    classList: {
      add: (name: string) => void classes.add(name),
      remove: (name: string) => void classes.delete(name),
      contains: (name: string) => classes.has(name),
    },
    matches: (selector: string) => selector === ".reveal-in",
    querySelectorAll: () => nested,
    getBoundingClientRect: () => ({ top, bottom: top + 100 }),
  };
}

const root = (children: Stub[]) => ({ querySelectorAll: () => children }) as unknown as ParentNode;

(globalThis as { HTMLElement?: unknown }).HTMLElement = class {};

let observed: Stub[];
let callbacks: ((entries: unknown[], observer: unknown) => void)[];
let thresholds: unknown[];

beforeEach(() => {
  observed = [];
  callbacks = [];
  thresholds = [];
  (globalThis as { window?: unknown }).window = { innerHeight: 1000 };
  (globalThis as { IntersectionObserver?: unknown }).IntersectionObserver = class {
    #live = true;
    constructor(cb: (entries: unknown[], observer: unknown) => void, options: { threshold?: unknown }) {
      callbacks.push(cb);
      thresholds.push(options.threshold);
    }
    observe(target: Stub) {
      if (this.#live) observed.push(target);
    }
    disconnect() {
      this.#live = false;
      observed = [];
    }
  };
});

afterEach(() => {
  delete (globalThis as { window?: unknown }).window;
  delete (globalThis as { IntersectionObserver?: unknown }).IntersectionObserver;
});

const { observeReveal } = await import("../lib/reveal");

test("nothing is hidden before observeReveal runs", () => {
  const below = el(4000);

  expect(below.classes.has("is-pending")).toBe(false);
  expect(below.classes.has("is-revealed")).toBe(false);
});

test("below-fold elements turn pending only at setup and are observed, on-screen ones go straight to revealed", () => {
  const above = el(200);
  const below = el(4000);
  observeReveal(root([above, below]));

  expect(above.classes.has("is-revealed")).toBe(true);
  expect(above.classes.has("is-pending")).toBe(false);
  expect(below.classes.has("is-revealed")).toBe(false);
  expect(below.classes.has("is-pending")).toBe(true);
  expect(observed).toEqual([below]);
});

test("default threshold is 0 so an element taller than the viewport still reveals", () => {
  observeReveal(root([el(4000)]));

  expect(thresholds).toEqual([0]);
});

function intersect(...targets: Stub[]): Stub[] {
  const unobserved: Stub[] = [];
  callbacks[0]!(
    targets.map((target) => ({ isIntersecting: true, target })),
    { unobserve: (t: Stub) => void unobserved.push(t) },
  );
  return unobserved;
}

test("an intersecting entry swaps pending for revealed; only a non-repeating one is unobserved", () => {
  const once = el(4000);
  const repeat = el(4000, [], ["reveal-in", "reveal-in--repeat"]);
  observeReveal(root([once, repeat]));

  expect(intersect(once, repeat)).toEqual([once]);
  expect(once.classes.has("is-revealed")).toBe(true);
  expect(once.classes.has("is-pending")).toBe(false);
  expect(repeat.classes.has("is-revealed")).toBe(true);
  expect(repeat.classes.has("is-pending")).toBe(false);
});

test("revealing a parent reveals its nested items with it, not one by one", () => {
  const item = el(4300);
  const card = el(4000, [item]);
  observeReveal(root([card, item]));

  expect(intersect(card)).toEqual([card, item]);
  expect(item.classes.has("is-revealed")).toBe(true);
  expect(item.classes.has("is-pending")).toBe(false);
});

test("a parent on screen at load reveals nested items that hang below the fold, never pending", () => {
  const item = el(1200);
  const card = el(800, [item]);
  observeReveal(root([card, item]));

  expect(item.classes.has("is-revealed")).toBe(true);
  expect(item.classes.has("is-pending")).toBe(false);
  expect(observed).toEqual([]);
});

test("a second scan over the same elements does not double-observe", () => {
  const below = el(4000);
  observeReveal(root([below]));
  observeReveal(root([below]));

  expect(observed).toEqual([below]);
});

test("every rect is read before any class is written", () => {
  const log: string[] = [];
  const spy = (name: string, top: number): Stub => {
    const target = el(top);
    const read = target.getBoundingClientRect;
    target.getBoundingClientRect = () => {
      log.push(`read ${name}`);
      return read();
    };
    target.classList.add = (cls: string) => void log.push(`write ${name} ${cls}`);
    return target;
  };
  observeReveal(root([spy("a", 200), spy("b", 300), spy("c", 4000)]));

  expect(log).toEqual([
    "read a",
    "read b",
    "read c",
    "write a is-revealed",
    "write b is-revealed",
    "write c is-pending",
  ]);
});

test("dispose releases the handled set so a re-scan re-wires", () => {
  const below = el(4000);
  const dispose = observeReveal(root([below]));
  dispose();

  expect(observed).toEqual([]);

  observeReveal(root([below]));
  expect(observed).toEqual([below]);
});
