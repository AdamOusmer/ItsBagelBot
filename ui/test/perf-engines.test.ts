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
const queries = new Map<string, ReturnType<typeof target> & { matches: boolean }>();
const media = (feature: string) => {
  matchMediaCalls += 1;
  const query = Object.assign(target(), {
    matches: !feature.includes('reduced-motion'),
  });
  queries.set(feature, query);
  return query;
};

const win = Object.assign(target(), { innerWidth: 1000, innerHeight: 500, matchMedia: media });
let cursorEnabled = false;
const doc = Object.assign(target(), {
  hidden: false,
  documentElement: { classList: {
    toggle(_name: string, enabled: boolean) { cursorEnabled = enabled; },
    remove() { cursorEnabled = false; },
  } },
  createElement: (tag: string) => {
    if (tag === 'div') return { style: {} as Record<string, string>, className: '', append() {}, remove: () => removed.push('div') };
    const canvas: StubCanvas = {
      style: {},
      className: '',
      width: 0,
      height: 0,
      setAttribute() {},
      remove() {},
      getContext: () => ctx,
    };
    const ctx: StubContext = {
      fills: [],
      fillStyle: '',
      setTransform() {},
      clearRect() {},
      beginPath() {},
      arc() {},
      fill() {
        ctx.fills.push(ctx.fillStyle);
      },
      canvas,
    };
    contexts.push(ctx);
    return canvas;
  },
});
type StubCanvas = {
  style: Record<string, string>;
  className: string;
  width: number;
  height: number;
  setAttribute(): void;
  remove(): void;
  getContext: () => StubContext;
};
type StubContext = {
  fills: string[];
  fillStyle: string;
  setTransform(): void;
  clearRect(): void;
  beginPath(): void;
  arc(): void;
  fill(): void;
  canvas: StubCanvas;
};
const contexts: StubContext[] = [];
const removed: string[] = [];

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
  'cursor-engine.ts',
  'cursor-prop.ts',
  'light-field.ts',
  'count-up.ts',
]);
const { mountCursor } = await lib.load<typeof import('../lib/cursor-engine')>('cursor-engine.ts');
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
  for (const [feature, query] of queries) query.matches = !feature.includes('reduced-motion');
});

describe('cursor', () => {
  function stage(top = (_read: number) => 100) {
    let rects = 0;
    const operations: string[] = [];
    const link = {
      isConnected: true,
      parentElement: null,
      matches: (selector: string) => selector === 'a, button, .bb-input',
      closest: () => link,
      getBoundingClientRect: () => {
        operations.push('measure');
        rects += 1;
        return { left: 100, top: top(rects), width: 80, height: 20 };
      },
    };
    const paint = () => ({
      style: new Proxy({} as Record<string, string>, {
        set(styles, name: string, value: string) {
          // Browsers skip style invalidation for an unchanged declaration.
          if (styles[name] !== value) operations.push(name);
          styles[name] = value;
          return true;
        },
      }),
      classList: { toggle() {} },
    });
    computed = { borderRadius: '8px', getPropertyValue: () => '' };
    const ring = paint();
    const dot = paint();
    const fill = paint();
    const dispose = mountCursor({ dot: dot as never, ring: ring as never, fill: fill as never });
    return { link, ring, dot, dispose, operations, rects: () => rects };
  }

  test('a hovered element that moves mid-hover is tracked, then the loop sleeps on the settled ring', () => {
    const { link, ring, dispose } = stage((read) => (read < 3 ? 100 : 97));
    doc.dispatch('pointerover', { target: link });
    settle();
    expect(styleReads).toBe(1);
    expect(frames).toHaveLength(0);
    expect([ring.style.transform, ring.style.width, ring.style.height]).toEqual([
      'translate(94.000px, 91.000px) scale(1, 1)',
      '92px',
      '32px',
    ]);
    dispose();
  });

  test('measures before writing styles and keeps layout dimensions fixed while edges warp', () => {
    const { link, dispose, operations } = stage();
    doc.dispatch('pointerover', { target: link });
    flush(1000);
    expect(operations[0]).toBe('measure');
    settle();
    for (const property of ['width', 'height']) {
      expect(operations.filter((op) => op === property)).toHaveLength(2);
    }
    operations.length = 0;
    win.dispatch('pointermove', { clientX: 120, clientY: 110 });
    settle(2000);
    expect(operations[0]).toBe('measure');
    expect(operations.filter((op) => ['width', 'height'].includes(op))).toEqual([]);
    expect(frames).toHaveLength(0);
    dispose();
  });

  test('free movement batches pointer events into one frame and sleeps as soon as it settles', () => {
    const { dot, dispose } = stage();
    settle();
    for (let i = 0; i < 100; i++) win.dispatch('pointermove', { clientX: i, clientY: i });
    expect(frames).toHaveLength(1);
    const start = performance.now();
    const end = settle(start);
    expect(dot.style.transform).toBe('translate(99px, 99px)');
    expect(end - start).toBeLessThan(500);
    expect(frames).toHaveLength(0);
    dispose();
  });

  test('easing covers the same distance in the same time at 30 and 60 Hz', () => {
    const position = (interval: number) => {
      const { ring, dispose } = stage();
      win.dispatch('pointermove', { clientX: 500, clientY: 250 });
      settle();
      win.dispatch('pointermove', { clientX: 900, clientY: 450 });
      flush(1000);
      for (let elapsed = interval; elapsed <= 100 + 0.001; elapsed += interval) flush(1000 + elapsed);
      const transform = ring.style.transform;
      dispose();
      return transform;
    };
    expect(position(1000 / 30)).toBe(position(1000 / 60));
  });

  test('media changes restore the native cursor and resume when eligible again', () => {
    const { link, dispose, rects } = stage();
    settle();
    const pointer = queries.get('(min-width: 901px) and (hover: hover) and (pointer: fine)')!;
    const motion = queries.get('(prefers-reduced-motion: reduce)')!;
    expect(cursorEnabled).toBe(true);
    for (const [query, disabled] of [[pointer, false], [motion, true]] as const) {
      query.matches = disabled;
      query.dispatch('change');
      expect(cursorEnabled).toBe(false);
      win.dispatch('pointermove', { clientX: 10, clientY: 20 });
      doc.dispatch('pointerover', { target: link });
      expect(frames).toHaveLength(0);
      expect(rects()).toBe(0);
      query.matches = !disabled;
      query.dispatch('change');
      expect(cursorEnabled).toBe(true);
      settle();
    }
    dispose();
    expect([pointer.count('change'), motion.count('change')]).toEqual([0, 0]);
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
    const fill = layer('fill');
    const dispose = mountCursor({ dot: dot as never, ring: ring as never, fill: fill as never });
    expect([dot.popover, ring.popover, fill.popover, shown]).toEqual(['manual', 'manual', 'manual', ['fill', 'ring', 'dot']]);

    doc.dispatch('beforetoggle', { newState: 'open', target: {} });
    expect(shown).toHaveLength(3);
    await Promise.resolve();
    expect(shown).toEqual(['fill', 'ring', 'dot', 'fill', 'ring', 'dot']);
    doc.dispatch('beforetoggle', { newState: 'closed', target: {} });
    doc.dispatch('beforetoggle', { newState: 'open', target: dot });
    await Promise.resolve();
    expect(shown).toHaveLength(6);

    dispose();
    expect([dot.popover, ring.popover, fill.popover, doc.count('beforetoggle')]).toEqual([null, null, null, 0]);
  });
});

describe('light field', () => {
  function host(width: number) {
    const children: unknown[] = [];
    return {
      clientWidth: width,
      clientHeight: 400,
      children,
      replaceChildren: (...nodes: unknown[]) => children.splice(0, children.length, ...nodes),
    };
  }

  const mount = (el: ReturnType<typeof host>, options?: { warmth: number }) => {
    const before = contexts.length;
    const stop = field(el as unknown as HTMLElement, options);
    return { stop, layers: contexts.slice(before) };
  };

  const dots = (layers: Array<{ fills: string[] }>) => layers.reduce((n, l) => n + l.fills.length, 0) / 3;

  test('mote colours come from the custom properties once, with the literals as fallback', () => {
    computed = { getPropertyValue: (name: string) => (name === '--bb-tan-rgb' ? ' 10, 20, 30 ' : '') };
    const warm = mount(host(1000), { warmth: 1 });
    const warmFills = warm.layers.flatMap((l) => l.fills);
    expect(dots(warm.layers)).toBe(70);
    expect(warmFills.every((f) => f.startsWith('rgba(10, 20, 30, '))).toBe(true);

    const cool = mount(host(1000), { warmth: 0 });
    expect(cool.layers.flatMap((l) => l.fills).every((f) => f.startsWith('rgba(98, 165, 147, '))).toBe(true);
    expect(styleReads).toBe(4);
    warm.stop?.();
    cool.stop?.();
  });

  test('three layers are drawn once, never touch the frame loop, and drift at height over speed', () => {
    computed = { getPropertyValue: () => '' };
    const el = host(1000);
    const { stop, layers } = mount(el);
    expect(layers).toHaveLength(3);
    expect(el.children).toHaveLength(3);
    expect(dots(layers)).toBe(70);
    expect(layers.map((l) => l.fills.length / 3)).toEqual([24, 23, 23]);
    expect(frames).toHaveLength(0);
    expect(isRunning()).toBe(false);
    flush(1016);
    flush(1032);
    expect(dots(layers)).toBe(70);

    const seconds = layers.map((l) => parseFloat(l.canvas.style.animationDuration));
    expect(seconds).toEqual([0.07, 0.14, 0.24].map((v) => Number((400 / v / 60).toFixed(2))));
    expect(layers.every((l) => parseFloat(l.canvas.style.animationDelay) <= 0)).toBe(true);
    expect(layers.every((l) => l.canvas.height === 800 && l.canvas.width === 1000)).toBe(true);
    stop?.();
  });

  test('narrow hosts get 40 motes', () => {
    computed = { getPropertyValue: () => '' };
    const { stop, layers } = mount(host(500));
    expect(dots(layers)).toBe(40);
    stop?.();
  });

  test('a burst of resizes redraws once, after the trailing debounce, and recomputes durations', () => {
    computed = { getPropertyValue: () => '' };
    const el = host(1000);
    const { stop, layers } = mount(el);
    expect(dots(layers)).toBe(70);

    el.clientWidth = 500;
    el.clientHeight = 800;
    for (let i = 0; i < 5; i++) observers.resize[observers.resize.length - 1]();
    expect(timers.size).toBe(1);
    fireTimers();
    expect(dots(layers)).toBe(70 + 40);
    expect(parseFloat(layers[0].canvas.style.animationDuration)).toBe(Number((800 / 0.07 / 60).toFixed(2)));

    observers.resize[observers.resize.length - 1]();
    stop?.();
    expect(timers.size).toBe(0);
  });

  test('stop removes every layer and cancels a pending redraw', () => {
    computed = { getPropertyValue: () => '' };
    const { stop } = mount(host(1000));
    removed.length = 0;
    observers.resize[observers.resize.length - 1]();
    expect(timers.size).toBe(1);
    stop?.();
    expect(removed).toHaveLength(3);
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
