// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, beforeEach, describe, expect, test } from 'bun:test';
import { copyFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const GLOBALS = ['window', 'document', 'requestAnimationFrame', 'cancelAnimationFrame'];
const saved = new Map(GLOBALS.map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)] as const));

let fine = true;
let reduced = false;

type Listener = (event: unknown) => void;

function target() {
  const listeners = new Map<string, Set<Listener>>();
  return {
    addEventListener(type: string, fn: Listener) {
      if (!listeners.has(type)) listeners.set(type, new Set());
      listeners.get(type)?.add(fn);
    },
    removeEventListener(type: string, fn: Listener) {
      listeners.get(type)?.delete(fn);
    },
    dispatch(type: string, event: unknown = {}) {
      for (const fn of listeners.get(type) ?? []) fn(event);
    },
    count(type: string) {
      return listeners.get(type)?.size ?? 0;
    },
  };
}

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

const dir = mkdtempSync(join(tmpdir(), 'bagel-pointer-test-'));
for (const file of ['motion-query.ts', 'raf-loop.ts', 'parallax.ts', 'tilt.ts']) {
  copyFileSync(new URL(`../lib/${file}`, import.meta.url), join(dir, file));
}
const { mountParallax } = (await import(join(dir, 'parallax.ts'))) as typeof import('../lib/parallax');
const { mountTilt, observeTilt } = (await import(join(dir, 'tilt.ts'))) as typeof import('../lib/tilt');

afterAll(() => {
  rmSync(dir, { recursive: true, force: true });
  for (const [key, descriptor] of saved) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else Reflect.deleteProperty(globalThis, key);
  }
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

describe('tilt', () => {
  test('mount zeroes the tilt, the pointer leans it and leaving resets it', () => {
    const el = node({ left: 0, top: 0, width: 200, height: 100 });
    el.dataset.tilt = '8';
    const dispose = mountTilt(el as unknown as HTMLElement);
    expect([el.vars.get('--tilt-x'), el.vars.get('--tilt-y')]).toEqual(['0deg', '0deg']);
    el.dispatch('pointermove', { clientX: 150, clientY: 0 });
    expect([el.vars.get('--tilt-x'), el.vars.get('--tilt-y')]).toEqual(['4.00deg', '2.00deg']);
    el.dispatch('pointerleave');
    expect(el.vars.get('--tilt-x')).toBe('0deg');
    dispose();
  });

  test('the default lean is 4 degrees and a second mount is a no-op until disposed', () => {
    const el = node({ left: 0, top: 0, width: 100, height: 100 });
    const dispose = mountTilt(el as unknown as HTMLElement);
    mountTilt(el as unknown as HTMLElement);
    expect(el.count('pointermove')).toBe(1);
    el.dispatch('pointermove', { clientX: 100, clientY: 50 });
    expect(el.vars.get('--tilt-y')).toBe('2.00deg');
    dispose();
    expect([el.count('pointermove'), el.dataset.tiltReady]).toEqual([0, undefined]);
  });

  test('reduced motion and coarse pointers ignore the pointer', () => {
    const el = node({ left: 0, top: 0, width: 100, height: 100 });
    const dispose = mountTilt(el as unknown as HTMLElement);
    reduced = true;
    el.dispatch('pointermove', { clientX: 100, clientY: 100 });
    reduced = false;
    fine = false;
    el.dispatch('pointermove', { clientX: 100, clientY: 100 });
    expect(el.vars.get('--tilt-x')).toBe('0deg');
    dispose();
  });

  test('observeTilt mounts every [data-tilt] under the root', () => {
    const a = node();
    const b = node();
    const root = { querySelectorAll: () => [a, b] } as unknown as ParentNode;
    const dispose = observeTilt(root);
    expect([a.count('pointermove'), b.count('pointermove')]).toEqual([1, 1]);
    dispose();
    expect([a.count('pointermove'), b.count('pointermove')]).toEqual([0, 0]);
  });
});
