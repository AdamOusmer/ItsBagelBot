// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { RATE_AVG_SECONDS, RATE_NOW_SECONDS, exactRateWindow, exactRateWindows, perSecond, type ExactRateSample } from './rates';

const TICK_MS = 2000;
const UNKNOWN = { msg: null, event: null };

function sample(messages: number, at: number, events = messages): ExactRateSample {
  return { messages: BigInt(messages), events: BigInt(events), at };
}

function stepped(stepEveryMs: number, perStep: number, untilMs: number): ExactRateSample[] {
  const samples: ExactRateSample[] = [];
  for (let at = 0; at <= untilMs; at += TICK_MS) {
    const total = Math.floor(at / stepEveryMs) * perStep;
    samples.push(sample(total, at, total * 2));
  }
  return samples;
}

function feed(samples: ExactRateSample[]) {
  const next = exactRateWindows();
  return samples.map(next);
}

describe('shared rate windows', () => {
  test('a total that moves in 5s steps never reads as a zero rate once the window fills', () => {
    const rates = feed(stepped(5000, 125, 120_000)).slice(30);
    for (const r of rates) {
      expect(r.now.msg).toBeGreaterThan(0);
      expect(r.avg.msg).toBeGreaterThan(20);
      expect(r.avg.msg).toBeLessThan(30);
    }
  });

  test('reports events and messages independently', () => {
    const last = feed(stepped(5000, 100, 60_000)).at(-1)!;
    expect(last.avg.event).toBeCloseTo(last.avg.msg! * 2, 5);
  });

  test('stays unknown until at least a second has passed', () => {
    const next = exactRateWindows();
    expect(next(sample(10, 0))).toEqual({ now: UNKNOWN, avg: UNKNOWN });
    expect(next(sample(20, 500))).toEqual({ now: UNKNOWN, avg: UNKNOWN });
    const third = next(sample(30, 1000));
    expect([third.now.msg, third.avg.msg]).toEqual([20, 20]);
  });

  test('a counter reset restarts the window instead of reading as a long zero', () => {
    const next = exactRateWindows();
    next(sample(1000, 0));
    next(sample(1300, 10_000));
    next(sample(0, 12_000));
    const rate = next(sample(60, 14_000));
    expect([rate.now.msg, rate.avg.msg]).toEqual([30, 30]);
  });

  test('the now window forgets samples older than it while the minute average keeps them', () => {
    const next = exactRateWindows();
    next(sample(0, 0));
    next(sample(1000, 1000));
    const rate = next(sample(1100, 12_000));
    expect(rate.now.msg).toBeCloseTo(100 / 11, 5);
    expect(rate.avg.msg).toBeCloseTo(1100 / 12, 5);
  });

  test('exact rate samples retain increments above the JavaScript safe integer', () => {
    const next = exactRateWindow(60_000);
    next({ messages: 9223372036854775700n, events: 9223372036854775700n, at: 0 });
    const rate = next({ messages: 9223372036854775800n, events: 9223372036854775800n, at: 2000 });
    expect(rate.msg).toBe(50);
    expect(rate.event).toBe(50);
  });

  test('now and avg use the windows ingress uses', () => {
    expect([RATE_NOW_SECONDS, RATE_AVG_SECONDS]).toEqual([10, 60]);
  });

  test('a burst shows in the now window while the minute average stays low', () => {
    const burst = Array.from({ length: 31 }, (_, i) => {
      const at = i * TICK_MS;
      return sample(at <= 50_000 ? at / 1000 : 50 + ((at - 50_000) / 1000) * 10, at);
    });
    const last = feed(burst).at(-1)!;
    expect(last.now.msg!).toBeGreaterThan(8);
    expect(last.avg.msg!).toBeLessThan(3);
  });

  test('perSecond divides a window count by the window length', () => {
    expect(perSecond(300, 60)).toBe(5);
    expect(perSecond(undefined, 60)).toBe(0);
    expect(perSecond(10, 0)).toBe(0);
  });
});
