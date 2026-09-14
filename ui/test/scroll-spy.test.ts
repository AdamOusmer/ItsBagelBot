// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, test } from 'bun:test';
import { mountScrollSpy } from '../lib/scroll-spy';

const originals = new Map(['window', 'requestAnimationFrame', 'cancelAnimationFrame'].map((key) => [key, Object.getOwnPropertyDescriptor(globalThis, key)]));
afterEach(() => {
  for (const [key, descriptor] of originals) {
    if (descriptor) Object.defineProperty(globalThis, key, descriptor);
    else Reflect.deleteProperty(globalThis, key);
  }
});

test('coalesces scrolling, follows the last passed section, and fully disposes before a route swap', () => {
  const events = new EventTarget();
  Object.assign(events, { innerHeight: 1000 });
  const frames = new Map<number, FrameRequestCallback>();
  let nextFrame = 0;
  Object.defineProperty(globalThis, 'window', { configurable: true, value: events });
  Object.defineProperty(globalThis, 'requestAnimationFrame', { configurable: true, value: (callback: FrameRequestCallback) => { frames.set(++nextFrame, callback); return nextFrame; } });
  Object.defineProperty(globalThis, 'cancelAnimationFrame', { configurable: true, value: (id: number) => frames.delete(id) });
  let secondTop = 600;
  const sections = [
    { id: 'first', getBoundingClientRect: () => ({ top: -500 }) },
    { id: 'second', getBoundingClientRect: () => ({ top: secondTop }) },
  ] as HTMLElement[];
  const active = new Set<string>();
  const links = new Map(['first', 'second'].map((id) => [id, { classList: { toggle: (_class: string, on: boolean) => on ? active.add(id) : active.delete(id) } } as unknown as HTMLElement]));
  const dispose = mountScrollSpy(sections, links);
  expect([...active]).toEqual(['first']);
  secondTop = 300;
  events.dispatchEvent(new Event('scroll'));
  events.dispatchEvent(new Event('resize'));
  expect(frames.size).toBe(1);
  for (const [id, callback] of frames) { frames.delete(id); callback(0); }
  expect([...active]).toEqual(['second']);
  events.dispatchEvent(new Event('scroll'));
  const pending = [...frames.values()][0];
  dispose();
  expect(frames.size).toBe(0);
  events.dispatchEvent(new Event('scroll'));
  events.dispatchEvent(new Event('resize'));
  expect(frames.size).toBe(0);
  secondTop = 600;
  pending(0);
  expect([...active]).toEqual(['second']);
  dispose();
});
