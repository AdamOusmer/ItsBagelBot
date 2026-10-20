// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, test } from 'bun:test';
import { warmOnIntent } from './warm-on-intent';

class FakeElement extends EventTarget {
  constructor(private readonly control: boolean) {
    super();
  }
  closest() {
    return this.control ? this : null;
  }
}

function fire(host: EventTarget, type: string, target: FakeElement) {
  const event = new Event(type);
  Object.defineProperty(event, 'target', { value: target });
  host.dispatchEvent(event);
}

const g = globalThis as Record<string, unknown>;
const saved = { Element: g.Element, window: g.window, document: g.document, navigator: g.navigator, ric: g.requestIdleCallback };

function stubBrowser(options: { saveData?: boolean; ready?: string } = {}) {
  const win = new EventTarget();
  const idle: Array<() => void> = [];
  g.Element = FakeElement;
  g.window = win;
  g.document = { readyState: options.ready ?? 'complete' };
  Object.defineProperty(globalThis, 'navigator', { value: { connection: { saveData: options.saveData === true } }, configurable: true });
  g.requestIdleCallback = (run: () => void) => idle.push(run);
  g.cancelIdleCallback = () => {};
  return { win, idle };
}

afterEach(() => {
  g.Element = saved.Element;
  g.window = saved.window;
  g.document = saved.document;
  Object.defineProperty(globalThis, 'navigator', { value: saved.navigator, configurable: true });
  g.requestIdleCallback = saved.ric;
});

test('loads once on the first control contact, ignoring later triggers', () => {
  const { idle } = stubBrowser();
  const host = new FakeElement(false);
  let loads = 0;
  warmOnIntent(host as unknown as HTMLElement, [async () => void loads++]);

  fire(host, 'pointerover', new FakeElement(false));
  expect(loads).toBe(0);
  fire(host, 'pointerover', new FakeElement(true));
  fire(host, 'focusin', new FakeElement(true));
  idle.forEach((run) => run());
  expect(loads).toBe(1);
});

test('loads once when idle after the window load event, with no interaction', () => {
  const { win, idle } = stubBrowser({ ready: 'loading' });
  let loads = 0;
  warmOnIntent(new FakeElement(false) as unknown as HTMLElement, [async () => void loads++]);

  expect(idle.length).toBe(0);
  win.dispatchEvent(new Event('load'));
  expect(loads).toBe(0);
  idle.forEach((run) => run());
  idle.forEach((run) => run());
  expect(loads).toBe(1);
});

test('does not idle-warm when the connection asks to save data', () => {
  const { idle } = stubBrowser({ saveData: true });
  warmOnIntent(new FakeElement(false) as unknown as HTMLElement, [async () => {}]);
  expect(idle.length).toBe(0);
});
