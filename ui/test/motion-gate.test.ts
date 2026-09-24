// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// observeMotion's wiring: every `[data-motion]` region is watched, flips with
// its intersection, and stops being watched on dispose. Bun has no DOM, so the
// two surfaces motion-gate.ts touches (querySelectorAll, IntersectionObserver)
// are stubbed, as in reveal.test.ts.

import { afterEach, beforeEach, expect, test } from "bun:test";
import { observeMotion } from "../lib/motion-gate";

type Region = { dataset: Record<string, string | undefined> };

let observed: Region[];
let callback: (entries: { target: Region; isIntersecting: boolean }[]) => void;
let disconnected: boolean;

beforeEach(() => {
  observed = [];
  disconnected = false;
  (globalThis as { IntersectionObserver?: unknown }).IntersectionObserver = class {
    constructor(cb: typeof callback) {
      callback = cb;
    }
    observe(target: Region) {
      observed.push(target);
    }
    disconnect() {
      disconnected = true;
    }
  };
});

afterEach(() => {
  delete (globalThis as { IntersectionObserver?: unknown }).IntersectionObserver;
});

const root = (regions: Region[]) => ({ querySelectorAll: () => regions }) as unknown as ParentNode;

test("watches every region and flips it with its intersection", () => {
  const near: Region = { dataset: {} };
  const far: Region = { dataset: {} };
  observeMotion(root([near, far]));

  expect(observed).toEqual([near, far]);
  callback([{ target: near, isIntersecting: true }, { target: far, isIntersecting: false }]);
  expect(near.dataset.motion).toBe("on");
  expect(far.dataset.motion).toBe("off");

  callback([{ target: far, isIntersecting: true }]);
  expect(far.dataset.motion).toBe("on");
});

test("dispose disconnects the observer", () => {
  const dispose = observeMotion(root([{ dataset: {} }]));
  dispose();
  expect(disconnected).toBe(true);
});

test("a page without regions creates no observer", () => {
  observeMotion(root([]));
  expect(observed).toEqual([]);
});
