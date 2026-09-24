// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, describe, expect, jest, test } from 'bun:test';
import { HIDDEN_GRACE_MS, IDLE_MS, visibleEventSource, type StreamHost } from './visible-stream';

class FakeSource {
  closed = false;
  constructor(readonly url: string) {}
  close() {
    this.closed = true;
  }
}

class FakeDoc extends EventTarget {
  hidden = false;
  set(hidden: boolean) {
    this.hidden = hidden;
    this.dispatchEvent(new Event('visibilitychange'));
  }
}

function setup(hidden = false) {
  const doc = new FakeDoc();
  doc.hidden = hidden;
  const win = new EventTarget();
  const sources: FakeSource[] = [];
  const attached: { es: FakeSource; reopened: boolean }[] = [];
  const host: StreamHost = {
    doc,
    win,
    connect: (url) => {
      const es = new FakeSource(url);
      sources.push(es);
      return es as unknown as EventSource;
    }
  };
  const stop = visibleEventSource(
    '/s',
    (es, reopened) => attached.push({ es: es as unknown as FakeSource, reopened }),
    host
  );
  const live = () => sources.filter((s) => !s.closed);
  return { doc, win, sources, attached, stop, live };
}

describe('visibleEventSource', () => {
  beforeEach(() => jest.useFakeTimers());
  afterEach(() => jest.useRealTimers());

  test('opens immediately on a visible page', () => {
    const t = setup();
    expect(t.sources.map((s) => s.url)).toEqual(['/s']);
    expect(t.attached[0].reopened).toBe(false);
  });

  test('a tab opened in the background waits until it is shown', () => {
    const t = setup(true);
    expect(t.sources).toHaveLength(0);
    t.doc.set(false);
    expect(t.live()).toHaveLength(1);
    expect(t.attached[0].reopened).toBe(false);
  });

  test('a quick tab switch keeps the same connection', () => {
    const t = setup();
    t.doc.set(true);
    jest.advanceTimersByTime(HIDDEN_GRACE_MS - 1);
    t.doc.set(false);
    jest.advanceTimersByTime(HIDDEN_GRACE_MS * 2);
    expect(t.sources).toHaveLength(1);
    expect(t.live()).toHaveLength(1);
  });

  test('closes after the grace period and reopens on return', () => {
    const t = setup();
    t.doc.set(true);
    jest.advanceTimersByTime(HIDDEN_GRACE_MS);
    expect(t.live()).toHaveLength(0);
    t.doc.set(false);
    expect(t.sources).toHaveLength(2);
    expect(t.live()).toEqual([t.sources[1]]);
    expect(t.attached[1].reopened).toBe(true);
  });

  test('repeated hidden events do not push the close out', () => {
    const t = setup();
    t.doc.set(true);
    jest.advanceTimersByTime(HIDDEN_GRACE_MS / 2);
    t.doc.set(true);
    jest.advanceTimersByTime(HIDDEN_GRACE_MS / 2);
    expect(t.live()).toHaveLength(0);
  });

  test('returning after repeated hidden events leaves no stray close armed', () => {
    const t = setup();
    t.doc.set(true);
    t.doc.set(true);
    t.doc.set(false);
    jest.advanceTimersByTime(HIDDEN_GRACE_MS * 2);
    expect(t.live()).toHaveLength(1);
  });

  test('a hidden tab stays closed for hours', () => {
    const t = setup();
    t.doc.set(true);
    jest.advanceTimersByTime(6 * 60 * 60 * 1000);
    expect(t.sources).toHaveLength(1);
    expect(t.live()).toHaveLength(0);
  });

  test('pagehide closes at once and pageshow reopens', () => {
    const t = setup();
    t.win.dispatchEvent(new Event('pagehide'));
    expect(t.live()).toHaveLength(0);
    t.win.dispatchEvent(new Event('pageshow'));
    expect(t.live()).toHaveLength(1);
    expect(t.attached[1].reopened).toBe(true);
  });

  test('pageshow on a hidden page does not open', () => {
    const t = setup();
    t.win.dispatchEvent(new Event('pagehide'));
    t.doc.hidden = true;
    t.win.dispatchEvent(new Event('pageshow'));
    expect(t.live()).toHaveLength(0);
  });

  test('pageshow on an open stream does not double-connect', () => {
    const t = setup();
    t.win.dispatchEvent(new Event('pageshow'));
    expect(t.sources).toHaveLength(1);
  });

  test('stop closes and detaches every listener', () => {
    const t = setup();
    t.doc.set(true);
    t.stop();
    expect(t.live()).toHaveLength(0);
    t.doc.set(false);
    t.win.dispatchEvent(new Event('pageshow'));
    t.win.dispatchEvent(new Event('pointermove'));
    jest.advanceTimersByTime(HIDDEN_GRACE_MS * 2);
    expect(t.sources).toHaveLength(1);
  });

  test('input on a hidden page does not cancel the hidden close', () => {
    const t = setup();
    t.doc.set(true);
    t.win.dispatchEvent(new Event('focus'));
    jest.advanceTimersByTime(HIDDEN_GRACE_MS);
    expect(t.live()).toHaveLength(0);
  });
});

describe('visibleEventSource idle pause', () => {
  beforeEach(() => jest.useFakeTimers());
  afterEach(() => jest.useRealTimers());

  test('a visible page with no input closes after the idle window', () => {
    const t = setup();
    jest.advanceTimersByTime(IDLE_MS - 1);
    expect(t.live()).toHaveLength(1);
    jest.advanceTimersByTime(1);
    expect(t.live()).toHaveLength(0);
  });

  test('input keeps the stream open without reconnecting', () => {
    const t = setup();
    for (let i = 0; i < 4; i++) {
      jest.advanceTimersByTime(IDLE_MS - 1000);
      t.win.dispatchEvent(new Event('pointermove'));
    }
    expect(t.sources).toHaveLength(1);
    expect(t.live()).toHaveLength(1);
  });

  test('the idle window restarts from the last input', () => {
    const t = setup();
    jest.advanceTimersByTime(IDLE_MS / 2);
    t.win.dispatchEvent(new Event('keydown'));
    jest.advanceTimersByTime(IDLE_MS - 1);
    expect(t.live()).toHaveLength(1);
    jest.advanceTimersByTime(1);
    expect(t.live()).toHaveLength(0);
  });

  test.each(['pointerdown', 'pointermove', 'keydown', 'wheel', 'touchstart', 'focus'])(
    '%s after idling reopens the stream',
    (type) => {
      const t = setup();
      jest.advanceTimersByTime(IDLE_MS);
      expect(t.live()).toHaveLength(0);
      t.win.dispatchEvent(new Event(type));
      expect(t.live()).toHaveLength(1);
      expect(t.attached[1].reopened).toBe(true);
    }
  );

  test('a reopened stream idles out again', () => {
    const t = setup();
    jest.advanceTimersByTime(IDLE_MS);
    t.win.dispatchEvent(new Event('pointerdown'));
    jest.advanceTimersByTime(IDLE_MS);
    expect(t.sources).toHaveLength(2);
    expect(t.live()).toHaveLength(0);
  });

  test('returning to the tab counts as input', () => {
    const t = setup();
    jest.advanceTimersByTime(IDLE_MS - 1000);
    t.doc.set(true);
    t.doc.set(false);
    jest.advanceTimersByTime(IDLE_MS - 1);
    expect(t.sources).toHaveLength(1);
    expect(t.live()).toHaveLength(1);
  });

  test('a tab idled out while hidden reopens on return', () => {
    const t = setup();
    t.doc.set(true);
    jest.advanceTimersByTime(IDLE_MS * 2);
    t.doc.set(false);
    expect(t.live()).toHaveLength(1);
    jest.advanceTimersByTime(IDLE_MS - 1);
    expect(t.live()).toHaveLength(1);
  });
});
