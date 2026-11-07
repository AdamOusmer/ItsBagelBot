// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, beforeEach, describe, expect, test } from 'bun:test';
import { eventTarget as target, isolatedLib, snapshotGlobals } from './dom-fakes';

const restoreGlobals = snapshotGlobals(['window', 'document', 'requestAnimationFrame', 'cancelAnimationFrame']);

let fine = true;
let reduced = false;

const media = (feature: string) => ({
  get matches() {
    return feature.includes('reduced-motion') ? reduced : fine;
  },
  addEventListener() {},
  removeEventListener() {},
});

const win = Object.assign(target(), { innerWidth: 1000, innerHeight: 500, matchMedia: media });

let frames: Array<(now: number) => void> = [];
Object.assign(globalThis, {
  window: win,
  document: { hidden: false, addEventListener() {}, removeEventListener() {} },
  requestAnimationFrame: (fn: (now: number) => void) => frames.push(fn),
  cancelAnimationFrame: () => {
    frames = [];
  },
});

function flush(): void {
  const due = frames;
  frames = [];
  for (const fn of due) fn(0);
}

const lib = isolatedLib('pointer', ['motion-query.ts', 'raf-loop.ts', 'rect-cache.ts', 'parallax.ts']);
const { mountParallax } = await lib.load<typeof import('../lib/parallax')>('parallax.ts');

afterAll(() => {
  lib.remove();
  restoreGlobals();
});

function node(rect = { left: 100, top: 50, width: 100, height: 40 }) {
  const vars = new Map<string, string>();
  return Object.assign(target(), {
    dataset: {} as Record<string, string | undefined>,
    style: { setProperty: (name: string, value: string) => vars.set(name, value) },
    vars,
    getBoundingClientRect: () => rect,
  });
}

beforeEach(() => {
  fine = true;
  reduced = false;
  flush();
});

describe('parallax', () => {
  test('element scope normalises the pointer to the node, once per frame, latest position wins', () => {
    const el = node();
    const seen: { px: number; py: number }[] = [];
    const dispose = mountParallax(el as unknown as HTMLElement, { onmove: (p) => seen.push(p) });
    flush();
    el.dispatch('pointermove', { clientX: 120, clientY: 60 });
    el.dispatch('pointermove', { clientX: 150, clientY: 60 });
    expect(seen).toEqual([]);
    flush();
    expect(seen).toEqual([{ px: 0, py: -0.5 }]);
    flush();
    expect(seen).toHaveLength(1);
    dispose();
  });

  test('leaving recentres at once and drops the frame still pending', () => {
    const el = node();
    const seen: { px: number; py: number }[] = [];
    const dispose = mountParallax(el as unknown as HTMLElement, { onmove: (p) => seen.push(p) });
    el.dispatch('pointermove', { clientX: 200, clientY: 90 });
    el.dispatch('pointerleave');
    flush();
    expect(seen).toEqual([{ px: 0, py: 0 }]);
    dispose();
  });

  test('viewport scope follows the window and never recentres on leave', () => {
    const el = node();
    const seen: { px: number; py: number }[] = [];
    const dispose = mountParallax(el as unknown as HTMLElement, { scope: 'viewport', onmove: (p) => seen.push(p) });
    expect(el.count('pointermove')).toBe(0);
    expect(el.count('pointerleave')).toBe(0);
    win.dispatch('pointermove', { clientX: 750, clientY: 0 });
    flush();
    expect(seen).toEqual([{ px: 0.5, py: -1 }]);
    dispose();
    expect(win.count('pointermove')).toBe(0);
  });

  test('coarse pointers and reduced motion leave the scene still', () => {
    const el = node();
    const seen: unknown[] = [];
    const dispose = mountParallax(el as unknown as HTMLElement, { onmove: (p) => seen.push(p) });
    fine = false;
    el.dispatch('pointermove', { clientX: 150, clientY: 60 });
    flush();
    fine = true;
    reduced = true;
    el.dispatch('pointermove', { clientX: 150, clientY: 60 });
    flush();
    expect(seen).toEqual([]);
    dispose();
    expect([el.count('pointermove'), el.count('pointerleave')]).toEqual([0, 0]);
  });
});
