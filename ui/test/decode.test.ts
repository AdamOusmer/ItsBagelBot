// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, describe, expect, test } from 'bun:test';
import { isolatedLib, snapshotGlobals } from './dom-fakes';

const restoreGlobals = snapshotGlobals(['requestAnimationFrame', 'cancelAnimationFrame', 'IntersectionObserver', 'Element']);

let queue: FrameRequestCallback[] = [];
globalThis.requestAnimationFrame = ((callback: FrameRequestCallback) => {
  queue.push(callback);
  return queue.length;
}) as typeof requestAnimationFrame;
globalThis.cancelAnimationFrame = (() => {
  queue = [];
}) as typeof cancelAnimationFrame;

function frame(time: number): void {
  const pending = queue;
  queue = [];
  for (const callback of pending) callback(time);
}

const lib = isolatedLib('decode', ['decode.ts', 'raf-loop.ts', 'motion-query.ts']);
const { decode, observeDecode } = await lib.load<typeof import('../lib/decode')>('decode.ts');
const { isRunning, wake } = await lib.load<typeof import('../lib/raf-loop')>('raf-loop.ts');

afterAll(() => {
  lib.remove();
  restoreGlobals();
});

const textNode = () => ({ textContent: '' }) as unknown as HTMLElement;

describe('decode', () => {
  test('a finished decode unsubscribes itself and reports done once', () => {
    const el = textNode();
    let done = 0;
    decode(el, 'hello', { durationMs: 100 }, () => (done += 1));
    frame(performance.now() + 5000);
    expect([el.textContent, done]).toEqual(['hello', 1]);
    wake();
    expect(isRunning()).toBe(false);
  });

  test('one-shot scramble uses the given charset and duration, then settles', () => {
    const el = textNode();
    const t0 = performance.now();
    decode(el, 'moth_lamp', { charset: 'x', durationMs: 900 });
    frame(t0);
    expect(el.textContent).toBe('xxxxxxxxx');
    frame(t0 + 450);
    expect(el.textContent?.startsWith('moth')).toBe(true);
    frame(t0 + 905);
    expect(el.textContent).toBe('moth_lamp');
    expect(queue).toHaveLength(0);
  });

  test('default duration still follows text length', () => {
    const el = textNode();
    const t0 = performance.now();
    decode(el, 'abcdefghij', {});
    frame(t0 + 639);
    expect(el.textContent).not.toBe('abcdefghij');
    frame(t0 + 645);
    expect(el.textContent).toBe('abcdefghij');
  });

  test('stopping early leaves the frame where it was', () => {
    const el = textNode();
    const t0 = performance.now();
    const stop = decode(el, 'gg beans', { charset: '#' });
    frame(t0);
    stop();
    frame(t0 + 2000);
    expect(el.textContent).toBe('## #####');
  });

  test('observeDecode passes scramble options through', () => {
    class FakeElement {}
    globalThis.Element = FakeElement as unknown as typeof Element;
    globalThis.IntersectionObserver = class {
      constructor(private readonly callback: IntersectionObserverCallback) {}
      observe(el: Element) {
        this.callback([{ isIntersecting: true, target: el } as IntersectionObserverEntry], this as never);
      }
      unobserve() {}
      disconnect() {}
    } as unknown as typeof IntersectionObserver;

    const el = { textContent: 'tiny', dataset: { decode: 'soupdream' } } as unknown as HTMLElement;
    const root = { querySelectorAll: () => [el] } as unknown as ParentNode;
    const t0 = performance.now();
    const stop = observeDecode(root, { charset: 'z', durationMs: 900 });
    frame(t0);
    expect(el.textContent).toBe('zzzzzzzzz');
    stop();
  });
});
