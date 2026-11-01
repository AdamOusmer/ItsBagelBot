// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, expect, test } from 'bun:test';
import { eventTarget, isolatedLib, snapshotGlobals } from './dom-fakes';

const restoreGlobals = snapshotGlobals(['window', 'document', 'requestAnimationFrame', 'cancelAnimationFrame', 'ResizeObserver']);

let queue: Array<(time: number) => void> = [];
let now = 0;
globalThis.requestAnimationFrame = ((callback: FrameRequestCallback) => {
  queue.push(callback);
  return queue.length;
}) as typeof requestAnimationFrame;
globalThis.cancelAnimationFrame = (() => {}) as typeof cancelAnimationFrame;
Reflect.deleteProperty(globalThis, 'ResizeObserver');

const win = Object.assign(eventTarget(), { innerHeight: 500, scrollY: 0 });
(globalThis as { window?: unknown }).window = win;
let heightReads = 0;
const root = {
  get scrollHeight() {
    heightReads += 1;
    return 1500;
  },
};
(globalThis as { document?: unknown }).document = Object.assign(eventTarget(), { hidden: false, documentElement: root });

const lib = isolatedLib('reading-progress', ['reading-progress.ts', 'raf-loop.ts']);
afterAll(() => {
  lib.remove();
  restoreGlobals();
});
const { mountReadingProgress } = await lib.load<typeof import('../lib/reading-progress')>('reading-progress.ts');
const { wake } = await lib.load<typeof import('../lib/raf-loop')>('raf-loop.ts');

function frames(count = 2): void {
  for (let i = 0; i < count; i += 1) {
    const pending = queue;
    queue = [];
    now += 16;
    for (const callback of pending) callback(now);
  }
}

function fill() {
  const writes: string[] = [];
  return { writes, style: { setProperty: (_: string, value: string) => writes.push(value) } };
}

test('a disposed progress bar is never painted again, even when the shared loop wakes every subscriber', () => {
  const old = fill();
  const dispose = mountReadingProgress(old as unknown as HTMLElement);
  win.scrollY = 250;
  win.dispatch('scroll');
  frames();
  expect(old.writes.at(-1)).toBe('0.2500');

  dispose();
  const painted = old.writes.length;
  const current = fill();
  const disposeCurrent = mountReadingProgress(current as unknown as HTMLElement);

  win.scrollY = 500;
  win.dispatch('scroll');
  wake();
  frames();

  expect(old.writes.length).toBe(painted);
  expect(current.writes.at(-1)).toBe('0.5000');
  disposeCurrent();
});

test('an address bar resize reads no layout: the page height comes from the resize observer', () => {
  let notify: ((entries: unknown[]) => void) | null = null;
  globalThis.ResizeObserver = class {
    constructor(callback: (entries: unknown[]) => void) {
      notify = callback;
    }
    observe() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver;
  try {
    const bar = fill();
    const dispose = mountReadingProgress(bar as unknown as HTMLElement);
    notify!([{ borderBoxSize: [{ blockSize: 2000 }], contentRect: { height: 2000 } }]);
    frames();
    const reads = heightReads;

    win.scrollY = 600;
    for (const height of [520, 560, 600]) {
      win.innerHeight = height;
      win.dispatch('resize');
      frames(1);
    }

    expect(heightReads).toBe(reads);
    expect(bar.writes.at(-1)).toBe('0.4286');
    dispose();
  } finally {
    Reflect.deleteProperty(globalThis, 'ResizeObserver');
    win.innerHeight = 500;
  }
});
