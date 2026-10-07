// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, beforeEach, describe, expect, test } from 'bun:test';
import { eventTarget as target, isolatedLib, snapshotGlobals } from './dom-fakes';

const restoreGlobals = snapshotGlobals([
  'window',
  'document',
  'Element',
  'requestAnimationFrame',
  'cancelAnimationFrame',
  'setTimeout',
  'clearTimeout',
  'getComputedStyle',
  'IntersectionObserver',
  'ResizeObserver',
]);

let matchMediaCalls = 0;
const media = (feature: string) => {
  matchMediaCalls += 1;
  return {
    matches: !feature.includes('reduced-motion'),
    addEventListener() {},
    removeEventListener() {},
  };
};

const win = Object.assign(target(), { innerWidth: 1000, innerHeight: 500, matchMedia: media });
const doc = Object.assign(target(), {
  hidden: false,
  documentElement: { classList: { toggle() {}, remove() {} } },
  createElement: () => ({
    style: {} as Record<string, string>,
    animate: () => ({ cancel() {}, onfinish: null }),
  }),
});

let frames: Array<(now: number) => void> = [];
let timers = new Map<number, () => void>();
let nextTimer = 0;
let styleReads = 0;
let computed: Record<string, unknown> = {};
const observers = { intersection: [] as Array<(entries: unknown[]) => void>, resize: [] as Array<() => void> };

class FakeIntersectionObserver {
  constructor(callback: (entries: unknown[]) => void) {
    observers.intersection.push(callback);
  }
  observe() {}
  unobserve() {}
  disconnect() {}
}

class FakeResizeObserver {
  constructor(callback: () => void) {
    observers.resize.push(callback);
  }
  observe() {}
  disconnect() {}
}

Object.assign(globalThis, {
  window: win,
  document: doc,
  Element: class {},
  requestAnimationFrame: (fn: (now: number) => void) => frames.push(fn),
  cancelAnimationFrame: () => {
    frames = [];
  },
  setTimeout: (fn: () => void) => {
    timers.set(++nextTimer, fn);
    return nextTimer;
  },
  clearTimeout: (id: number) => void timers.delete(id),
  getComputedStyle: () => {
    styleReads += 1;
    return computed;
  },
  IntersectionObserver: FakeIntersectionObserver,
  ResizeObserver: FakeResizeObserver,
});

function flush(now = 0): void {
  const due = frames;
  frames = [];
  for (const fn of due) fn(now);
}

function settle(from = 1000, limit = 200): number {
  let now = from;
  for (let i = 0; i < limit && frames.length > 0; i++, now += 16) flush(now);
  return now;
}

function fireTimers(): void {
  const due = [...timers.values()];
  timers = new Map();
  for (const fn of due) fn();
}

const lib = isolatedLib('perf', [
  'motion-query.ts',
  'raf-loop.ts',
  'rect-cache.ts',
  'magnetic.ts',
  'cursor-engine.ts',
  'light-field.ts',
  'count-up.ts',
]);
const { mountCursor } = await lib.load<typeof import('../lib/cursor-engine')>('cursor-engine.ts');
const { mountMagnetic } = await lib.load<typeof import('../lib/magnetic')>('magnetic.ts');
const { field } = await lib.load<typeof import('../lib/light-field')>('light-field.ts');
const { countUp } = await lib.load<typeof import('../lib/count-up')>('count-up.ts');
const { isRunning, wake } = await lib.load<typeof import('../lib/raf-loop')>('raf-loop.ts');

afterAll(() => {
  lib.remove();
  restoreGlobals();
});

beforeEach(() => {
  frames = [];
  timers = new Map();
  styleReads = 0;
  computed = {};
  doc.hidden = false;
  observers.intersection = [];
  observers.resize = [];
});

describe('cursor', () => {
  function stage(top = (_read: number) => 100) {
    let rects = 0;
    const link = {
      isConnected: true,
      matches: () => false,
      closest: () => link,
      getBoundingClientRect: () => {
        rects += 1;
        return { left: 100, top: top(rects), width: 80, height: 20 };
      },
    };
    const paint = () => ({ style: {} as Record<string, string>, classList: { toggle() {} } });
    computed = { borderRadius: '8px' };
    const ring = paint();
    const dispose = mountCursor({ dot: paint() as never, ring: ring as never });
    return { link, ring, dispose, rects: () => rects };
  }

  test('a hovered element that moves mid-hover is tracked, then the loop sleeps on the settled ring', () => {
    const { link, ring, dispose } = stage((read) => (read < 3 ? 100 : 97));
    doc.dispatch('pointerover', { target: link });
    settle();
    expect(styleReads).toBe(1);
    expect(frames).toHaveLength(0);
    expect([ring.style.transform, ring.style.width, ring.style.height]).toEqual([
      'translate(94.0px, 91.0px)',
      '92.0px',
      '32.0px',
    ]);
    dispose();
  });

  test('scroll and resize remeasure the hovered element and settle again', () => {
    const { link, dispose, rects } = stage();
    doc.dispatch('pointerover', { target: link });
    settle();
    const hovered = rects();
    win.dispatch('scroll');
    expect(frames.length).toBeGreaterThan(0);
    settle();
    const scrolled = rects();
    win.dispatch('resize');
    settle();
    expect([scrolled > hovered, rects() > scrolled]).toEqual([true, true]);
    expect(styleReads).toBe(1);
    expect(frames).toHaveLength(0);
    dispose();
    expect([win.count('scroll'), win.count('resize')]).toEqual([0, 0]);
  });

  test('scroll with nothing hovered does not measure or wake the loop', () => {
    const { dispose, rects } = stage();
    settle();
    win.dispatch('scroll');
    expect(frames).toHaveLength(0);
    expect(rects()).toBe(0);
    dispose();
  });

  test('the cursor re-enters the top layer above any popover that opens', async () => {
    const shown: string[] = [];
    const layer = (name: string) => {
      let open = false;
      const el = {
        name,
        popover: null as string | null,
        style: {} as Record<string, string>,
        classList: { toggle() {} },
        matches: () => open,
        showPopover: () => {
          open = true;
          shown.push(name);
        },
        hidePopover: () => {
          open = false;
        },
        removeAttribute: (attr: string) => {
          if (attr === 'popover') el.popover = null;
          open = false;
        },
      };
      return el;
    };
    const dot = layer('dot');
    const ring = layer('ring');
    const dispose = mountCursor({ dot: dot as never, ring: ring as never });
    expect([dot.popover, ring.popover, shown]).toEqual(['manual', 'manual', ['ring', 'dot']]);

    doc.dispatch('beforetoggle', { newState: 'open', target: {} });
    expect(shown).toHaveLength(2);
    await Promise.resolve();
    expect(shown).toEqual(['ring', 'dot', 'ring', 'dot']);
    doc.dispatch('beforetoggle', { newState: 'closed', target: {} });
    doc.dispatch('beforetoggle', { newState: 'open', target: dot });
    await Promise.resolve();
    expect(shown).toHaveLength(4);

    dispose();
    expect([dot.popover, ring.popover, doc.count('beforetoggle')]).toEqual([null, null, 0]);
  });
});

describe('magnetic', () => {
  function stage() {
    let rects = 0;
    const writes = { transition: [] as string[], transform: 0, last: '' };
    const style = {
      set transition(value: string) {
        writes.transition.push(value);
      },
      set transform(value: string) {
        writes.transform += 1;
        writes.last = value;
      },
      willChange: '',
    };
    computed = { transform: 'none' };
    const el = Object.assign(target(), {
      style,
      getBoundingClientRect: () => {
        rects += 1;
        return { left: 0, top: 0, width: 100, height: 100 };
      },
    });
    const dispose = mountMagnetic(el as unknown as HTMLElement);
    return { el, dispose, writes, rects: () => rects };
  }

  test('rect is read once per hover, transition is set on enter and leave only, one style write per frame', () => {
    const { el, dispose, writes, rects } = stage();
    flush();
    writes.transform = 0;
    el.dispatch('pointerenter');
    for (const x of [60, 80, 100]) el.dispatch('pointermove', { clientX: x, clientY: 50 });
    flush();
    expect([rects(), writes.transform]).toEqual([1, 1]);
    expect(writes.transition).toEqual(['transform 0.3s cubic-bezier(0.16,1,0.3,1)', 'none']);
    win.dispatch('scroll');
    el.dispatch('pointermove', { clientX: 100, clientY: 50 });
    expect(rects()).toBe(2);
    el.dispatch('pointerleave');
    expect(writes.transition.at(-1)).toBe('transform 0.4s cubic-bezier(0.16,1,0.3,1)');
    expect(win.count('scroll')).toBe(0);
    dispose();
  });

  test('pull is measured from the displaced centre, as a live rect read would be', () => {
    const { el, dispose, writes } = stage();
    el.dispatch('pointerenter');
    el.dispatch('pointermove', { clientX: 100, clientY: 50 });
    flush();
    expect(writes.last).toBe('translate(2.80px, 0.00px)');
    el.dispatch('pointermove', { clientX: 60, clientY: 50 });
    flush();
    expect(writes.last).toBe('translate(2.67px, 0.00px)');
    dispose();
  });
});

describe('light field', () => {
  function host(width: number) {
    const children: unknown[] = [];
    const el = {
      clientWidth: width,
      clientHeight: 400,
      animate: () => ({ cancel() {}, onfinish: null }),
      append: (child: unknown) => void children.push(child),
      replaceChildren: () => void (children.length = 0),
    };
    return { el, children };
  }

  const backgrounds = (children: unknown[]) => children.map((c) => (c as { style: { background: string } }).style.background);

  test('mote colours come from the custom properties once, with the literals as fallback', () => {
    computed = { getPropertyValue: (name: string) => (name === '--bb-tan-rgb' ? ' 10, 20, 30 ' : '') };
    const warm = host(1000);
    field(warm.el as unknown as HTMLElement, { warmth: 1 });
    observers.intersection[0]([{ isIntersecting: true }]);
    expect(backgrounds(warm.children).every((b) => b.startsWith('rgba(10, 20, 30, '))).toBe(true);

    const cool = host(1000);
    field(cool.el as unknown as HTMLElement, { warmth: 0 });
    observers.intersection[1]([{ isIntersecting: true }]);
    expect(backgrounds(cool.children).every((b) => b.startsWith('rgba(82, 183, 136, '))).toBe(true);
    expect(styleReads).toBe(4);
  });

  test('a burst of resizes rebuilds once, after the trailing debounce', () => {
    computed = { getPropertyValue: () => '' };
    const { el, children } = host(1000);
    const dispose = field(el as unknown as HTMLElement);
    observers.intersection[0]([{ isIntersecting: true }]);
    expect(children).toHaveLength(70);

    el.clientWidth = 500;
    for (let i = 0; i < 5; i++) observers.resize[0]();
    expect(timers.size).toBe(1);
    expect(children).toHaveLength(70);
    fireTimers();
    expect(children).toHaveLength(40);

    observers.resize[0]();
    dispose?.();
    expect(timers.size).toBe(0);
  });
});

describe('count-up', () => {
  test('runs on the shared frame loop without touching matchMedia, and unsubscribes when done', () => {
    const node = { textContent: '900+' };
    const before = matchMediaCalls;
    const handle = countUp(node as unknown as HTMLElement, { durationMs: 1000 });
    expect(matchMediaCalls).toBe(before);
    expect(isRunning()).toBe(true);
    const t0 = performance.now();
    flush(t0 + 500);
    expect(node.textContent).toMatch(/^\d+\+$/);
    expect(node.textContent).not.toBe('900+');
    flush(t0 + 2000);
    expect(node.textContent).toBe('900+');
    wake();
    expect(isRunning()).toBe(false);
    handle?.destroy();
  });

  test('does not schedule a frame while the tab is hidden', () => {
    doc.hidden = true;
    const handle = countUp({ textContent: '900+' } as unknown as HTMLElement);
    expect(frames).toHaveLength(0);
    handle?.destroy();
    doc.hidden = false;
  });

  test('destroy stops the count', () => {
    const node = { textContent: '900+' };
    const handle = countUp(node as unknown as HTMLElement);
    handle?.destroy();
    wake();
    expect(isRunning()).toBe(false);
  });
});
