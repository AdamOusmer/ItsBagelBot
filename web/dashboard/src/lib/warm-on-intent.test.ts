// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
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

test('loads once, on the first pointer or focus contact with a control', () => {
  const originalElement = globalThis.Element;
  globalThis.Element = FakeElement as unknown as typeof Element;
  try {
    const host = new FakeElement(false);
    let loads = 0;
    const action = warmOnIntent(host as unknown as HTMLElement, [async () => void loads++]);

    fire(host, 'pointerover', new FakeElement(false));
    expect(loads).toBe(0);

    fire(host, 'pointerover', new FakeElement(true));
    fire(host, 'focusin', new FakeElement(true));
    expect(loads).toBe(1);

    action.destroy();
  } finally {
    globalThis.Element = originalElement;
  }
});
