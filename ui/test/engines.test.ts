// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, describe, expect, test } from 'bun:test';

import { isShortcut, isTyping, isTypingTarget } from '../lib/hotkeys';
import {
  axisValue,
  hoverTime,
  lineChart,
  nearestPoint,
  niceCeiling,
  plainValue,
  seriesClass,
  tooltipBox,
  type LinePoint,
} from '../lib/line-series';
import { livePoll } from '../lib/live-poll';

const el = (tagName: string, isContentEditable = false) => ({ tagName, isContentEditable }) as unknown as EventTarget;

const key = (target: EventTarget | null, mods: Partial<Record<'altKey' | 'ctrlKey' | 'metaKey', boolean>> = {}) =>
  ({ target, altKey: false, ctrlKey: false, metaKey: false, ...mods }) as unknown as KeyboardEvent;

describe('hotkeys', () => {
  test('form fields and editable regions count as typing', () => {
    for (const tag of ['INPUT', 'TEXTAREA', 'SELECT']) expect(isTypingTarget(el(tag))).toBe(true);
    expect(isTypingTarget(el('DIV', true))).toBe(true);
  });

  test('everything else, and no target, does not', () => {
    expect(isTypingTarget(el('BUTTON'))).toBe(false);
    expect(isTypingTarget(el('svg'))).toBe(false);
    expect(isTypingTarget(null)).toBe(false);
    expect(isTyping(key(el('A')))).toBe(false);
    expect(isTyping(key(el('INPUT')))).toBe(true);
  });

  test('a bare shortcut rejects typing and every modifier', () => {
    expect(isShortcut(key(el('BODY')))).toBe(true);
    expect(isShortcut(key(el('INPUT')))).toBe(false);
    expect(isShortcut(key(el('BODY'), { metaKey: true }))).toBe(false);
    expect(isShortcut(key(el('BODY'), { ctrlKey: true }))).toBe(false);
    expect(isShortcut(key(el('BODY'), { altKey: true }))).toBe(false);
  });

  test('an alt shortcut requires alt and still rejects ctrl, meta and typing', () => {
    expect(isShortcut(key(el('BODY'), { altKey: true }), { alt: true })).toBe(true);
    expect(isShortcut(key(el('BODY')), { alt: true })).toBe(false);
    expect(isShortcut(key(el('BODY'), { altKey: true, ctrlKey: true }), { alt: true })).toBe(false);
    expect(isShortcut(key(el('BODY'), { altKey: true, metaKey: true }), { alt: true })).toBe(false);
    expect(isShortcut(key(el('TEXTAREA'), { altKey: true }), { alt: true })).toBe(false);
  });
});

describe('line-series geometry', () => {
  test('ceilings round up to 1, 2, 4, 5 or 10 of the magnitude', () => {
    expect([0, -3, 0.3, 1, 1.5, 3, 4.2, 7, 12, 480].map(niceCeiling)).toEqual([1, 1, 0.4, 1, 2, 4, 5, 10, 20, 500]);
  });

  test('axis labels shorten thousands and keep small rates readable', () => {
    expect([0, 0.25, 2.5, 42, 1500, 25000].map(axisValue)).toEqual(['0', '0.25', '2.5', '42', '1.5k', '25k']);
  });

  test('missing and negative values format as empty', () => {
    expect([null, undefined, -1, Number.NaN, 3].map(plainValue)).toEqual(['', '', '', '', '3.0']);
  });

  const points: LinePoint[] = [
    { at: 40_000, a: 2, b: null },
    { at: 0, a: 1, b: 1 },
    { at: 10_000, a: null, b: 3 },
    { at: Number.NaN, a: 9, b: 9 },
    { at: 50_000, a: 4, b: 2 },
  ];

  test('readings sort by time, drop bad timestamps and break lines at gaps and nulls', () => {
    const chart = lineChart(points, ['a', 'b'], 800);
    expect(chart.readings.map((p) => p.at)).toEqual([0, 10_000, 40_000, 50_000]);
    expect(chart.ceiling).toBe(4);
    expect(chart.paths[0].trim().split(/\s+/).map((c) => c[0])).toEqual(['M', 'M', 'L']);
    expect(chart.paths[1].trim().split(/\s+/).map((c) => c[0])).toEqual(['M', 'L', 'M']);
    expect(chart.dots).toHaveLength(6);
    expect(chart.dotRadius).toBe(1.5);
    expect(chart.sampling).toBe(false);
  });

  test('wide charts get five time ticks, narrow ones three and a shorter plot', () => {
    const wide = lineChart(points, ['a'], 800);
    const narrow = lineChart(points, ['a'], 320);
    expect([wide.width, wide.height, wide.timeTicks.length]).toEqual([800, 260, 5]);
    expect([narrow.width, narrow.height, narrow.timeTicks.length]).toEqual([320, 220, 3]);
    expect(narrow.timeTicks.map((t) => t.anchor)).toEqual(['start', 'middle', 'end']);
    expect(lineChart(points, ['a'], 100).width).toBe(280);
  });

  test('fewer than two plotted readings is still sampling, and no readings means no time axis', () => {
    expect(lineChart([{ at: 1, a: 3 }], ['a'], 800).sampling).toBe(true);
    expect(lineChart([], ['a'], 800).timeTicks).toEqual([]);
  });

  test('hover resolves to the nearest reading and the tooltip stays inside the plot', () => {
    const chart = lineChart(points, ['a'], 800);
    expect(hoverTime(chart, 0)).toBe(chart.startAt);
    expect(hoverTime(chart, 10_000)).toBe(chart.startAt + chart.duration);
    expect(nearestPoint(chart.readings, 41_000)?.at).toBe(40_000);
    expect(nearestPoint([], 1)).toBeNull();
    expect(tooltipBox(chart, 10, 2)).toEqual({ x: 60, y: 31, width: 185, height: 72 });
    expect(tooltipBox(chart, 790, 1)).toEqual({ x: 604, y: 31, width: 185, height: 52 });
  });

  test('series classes carry tone and dash modifiers', () => {
    expect(seriesClass({ key: 'a', label: 'A' }, 'line')).toBe('bb-line-series__line bb-line-series__line--green');
    expect(seriesClass({ key: 'b', label: 'B', tone: 'tan', dashed: true }, 'key')).toBe(
      'bb-line-series__key bb-line-series__key--tan bb-line-series__key--dashed',
    );
  });
});

type Timer = { at: number; ms: number; fn: () => void };

function clock() {
  let now = 0;
  const queue: Timer[] = [];
  const realTimeout = globalThis.setTimeout;
  const realClear = globalThis.clearTimeout;
  globalThis.setTimeout = ((fn: () => void, ms: number) => {
    const entry = { at: now + ms, ms, fn };
    queue.push(entry);
    return entry;
  }) as unknown as typeof setTimeout;
  globalThis.clearTimeout = ((handle: unknown) => {
    const i = queue.indexOf(handle as Timer);
    if (i >= 0) queue.splice(i, 1);
  }) as unknown as typeof clearTimeout;
  return {
    now: () => now,
    queue,
    async settle() {
      for (let i = 0; i < 4; i += 1) await Promise.resolve();
    },
    async fire() {
      const due = queue.shift();
      if (due) now = due.at;
      due?.fn();
      await this.settle();
    },
    restore() {
      globalThis.setTimeout = realTimeout;
      globalThis.clearTimeout = realClear;
    },
  };
}

function page(hidden = false) {
  const listeners = new Set<() => void>();
  const doc = {
    hidden,
    addEventListener: (_: string, fn: () => void) => listeners.add(fn),
    removeEventListener: (_: string, fn: () => void) => listeners.delete(fn),
  };
  (globalThis as { document?: unknown }).document = doc;
  return {
    listeners,
    set(value: boolean) {
      doc.hidden = value;
      for (const fn of [...listeners]) fn();
    },
  };
}

describe('livePoll', () => {
  const hadDocument = Object.getOwnPropertyDescriptor(globalThis, 'document');
  afterEach(() => {
    if (hadDocument) Object.defineProperty(globalThis, 'document', hadDocument);
    else Reflect.deleteProperty(globalThis, 'document');
  });

  test('stops on the first settled tick and reports done once', async () => {
    const c = clock();
    let calls = 0;
    let done = 0;
    livePoll(async () => ++calls === 2, {
      firstDelayMs: 500,
      delayMs: () => 1000,
      timeoutMs: 30_000,
      onDone: () => (done += 1),
      now: c.now,
    });
    await c.fire();
    expect(c.queue.map((t) => t.ms)).toEqual([1000]);
    await c.fire();
    c.restore();
    expect([calls, done, c.queue.length]).toEqual([2, 1, 0]);
  });

  test('gives up at the deadline even while the backend never settles', async () => {
    const c = clock();
    let calls = 0;
    let done = 0;
    livePoll(async () => (calls += 1) < 0, {
      firstDelayMs: 0,
      delayMs: () => 1000,
      timeoutMs: 2000,
      onDone: () => (done += 1),
      now: c.now,
    });
    await c.fire();
    await c.fire();
    await c.fire();
    c.restore();
    expect([calls, done, c.queue.length]).toEqual([3, 1, 0]);
  });

  test('schedules nothing more once stopped mid-flight', async () => {
    const c = clock();
    let release: () => void = () => {};
    let done = 0;
    const stop = livePoll(
      () => new Promise<boolean>((resolve) => (release = () => resolve(false))),
      { firstDelayMs: 100, delayMs: () => 100, timeoutMs: 30_000, onDone: () => (done += 1), now: c.now },
    );
    await c.fire();
    stop();
    release();
    await c.settle();
    stop();
    c.restore();
    expect([done, c.queue.length]).toEqual([1, 0]);
  });

  test('a hidden page waits hiddenDelayMs between ticks', async () => {
    const c = clock();
    const vis = page(true);
    const stop = livePoll(async () => false, {
      firstDelayMs: 5000,
      delayMs: () => 5000,
      timeoutMs: Number.POSITIVE_INFINITY,
      hiddenDelayMs: 15_000,
      now: c.now,
    });
    await c.fire();
    expect(c.queue.map((t) => t.ms)).toEqual([15_000]);
    vis.set(false);
    await c.fire();
    expect(c.queue.map((t) => t.ms)).toEqual([5000]);
    stop();
    c.restore();
  });

  test('coming back to the page ticks at once and restarts the wait', async () => {
    const c = clock();
    const vis = page(true);
    let calls = 0;
    const stop = livePoll(async () => (calls += 1) < 0, {
      firstDelayMs: 5000,
      delayMs: () => 5000,
      timeoutMs: Number.POSITIVE_INFINITY,
      refreshOnVisible: true,
      now: c.now,
    });
    vis.set(true);
    expect(calls).toBe(0);
    vis.set(false);
    await c.settle();
    expect(calls).toBe(1);
    expect(c.queue).toHaveLength(1);
    stop();
    c.restore();
    expect(vis.listeners.size).toBe(0);
  });

  test('a visibility refresh never overlaps a tick already in flight', async () => {
    const c = clock();
    const vis = page(false);
    let calls = 0;
    let release: () => void = () => {};
    const stop = livePoll(
      () => {
        calls += 1;
        return new Promise<boolean>((resolve) => (release = () => resolve(false)));
      },
      { firstDelayMs: 100, delayMs: () => 100, timeoutMs: Number.POSITIVE_INFINITY, refreshOnVisible: true, now: c.now },
    );
    await c.fire();
    vis.set(false);
    expect(calls).toBe(1);
    release();
    await c.settle();
    expect(c.queue).toHaveLength(1);
    stop();
    c.restore();
  });

  test('without refreshOnVisible the page is never watched', () => {
    const c = clock();
    const vis = page(false);
    const stop = livePoll(async () => true, { firstDelayMs: 1, delayMs: () => 1, timeoutMs: 1, now: c.now });
    expect(vis.listeners.size).toBe(0);
    stop();
    c.restore();
  });
});
