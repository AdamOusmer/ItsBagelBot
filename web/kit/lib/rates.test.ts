// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { RATE_AVG_SECONDS, RATE_NOW_SECONDS, perSecond, rateWindow, rateWindows, type RateSample as Sample } from './rates';

const TICK_MS = 2000;

function stepped(stepEveryMs: number, perStep: number, untilMs: number, startAt = 0): Sample[] {
  const samples: Sample[] = [];
  for (let at = startAt; at <= untilMs; at += TICK_MS) {
    const steps = Math.floor(at / stepEveryMs);
    samples.push({ messages: steps * perStep, events: steps * perStep * 2, at });
  }
  return samples;
}

function feed(samples: Sample[], windowMs = 30_000) {
  const next = rateWindow(windowMs);
  return samples.map(next);
}

describe('rateWindow', () => {
  test('a total that moves in 5s steps never reads as a zero rate once the window fills', () => {
    const rates = feed(stepped(5000, 125, 120_000)).slice(15);
    for (const r of rates) {
      expect(r.msg).toBeGreaterThan(20);
      expect(r.msg).toBeLessThan(30);
    }
  });

  test('a 2s window over the same steps would read zero most of the time', () => {
    const rates = feed(stepped(5000, 125, 120_000), TICK_MS).slice(1);
    const zeros = rates.filter((r) => r.msg === 0).length;
    expect(zeros / rates.length).toBeGreaterThan(0.4);
  });

  test('reports events and messages independently', () => {
    const last = feed(stepped(5000, 100, 60_000)).at(-1)!;
    expect(last.event).toBeCloseTo(last.msg! * 2, 5);
  });

  test('stays unknown until at least a second has passed', () => {
    const next = rateWindow();
    expect(next({ messages: 10, events: 10, at: 0 })).toEqual({ msg: null, event: null });
    expect(next({ messages: 20, events: 20, at: 500 })).toEqual({ msg: null, event: null });
    expect(next({ messages: 30, events: 30, at: 1000 }).msg).toBe(20);
  });

  test('a counter reset restarts the window instead of reading as a long zero', () => {
    const next = rateWindow();
    next({ messages: 1000, events: 1000, at: 0 });
    next({ messages: 1300, events: 1300, at: 10_000 });
    next({ messages: 0, events: 0, at: 12_000 });
    expect(next({ messages: 60, events: 60, at: 14_000 }).msg).toBe(30);
  });

  test('forgets samples older than the window', () => {
    const next = rateWindow(10_000);
    next({ messages: 0, events: 0, at: 0 });
    next({ messages: 1000, events: 1000, at: 1000 });
    const r = next({ messages: 1100, events: 1100, at: 12_000 });
    expect(r.msg).toBeCloseTo(100 / 11, 5);
  });
});

describe('shared rate windows', () => {
  test('now and avg use the windows ingress uses', () => {
    expect(RATE_NOW_SECONDS).toBe(10);
    expect(RATE_AVG_SECONDS).toBe(60);
  });

  test('a burst shows in the now window while the minute average stays low', () => {
    const next = rateWindows();
    let last = next({ messages: 0, events: 0, at: 0 });
    for (let at = 2000; at <= 60_000; at += 2000) {
      const messages = at <= 50_000 ? at / 1000 : 50 + (at - 50_000) / 1000 * 10;
      last = next({ messages, events: messages, at });
    }
    expect(last.now.msg!).toBeGreaterThan(8);
    expect(last.avg.msg!).toBeLessThan(3);
  });

  test('perSecond divides a window count by the window length', () => {
    expect(perSecond(300, 60)).toBe(5);
    expect(perSecond(undefined, 60)).toBe(0);
    expect(perSecond(10, 0)).toBe(0);
  });
});
