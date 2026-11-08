// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { StatsCounter } from './stats-odometer';

describe('stats counter rate clock', () => {
  for (const rate of [1, 3, 14, 25]) {
    test.each([30, 60, 144])(`${rate}/s adds ${rate} in EACH single second at %i fps with two-second polling`, (fps) => {
      const counter = new StatsCounter(1_500_000_000, rate, 0);
      let previous = counter.read(0).total;
      for (let second = 1; second <= 20; second++) {
        for (let frame = 1; frame <= fps; frame++) {
          const now = (second - 1) * 1000 + frame * 1000 / fps;
          if (frame === fps && second % 2 === 0) counter.update(rate, now);
          const reading = counter.read(now);
          expect(reading.rate).toBe(rate);
          expect(reading.total).toBeCloseTo(1_500_000_000 + rate * now / 1000, 5);
        }
        const current = counter.read(second * 1000).total;
        expect(current - previous).toBe(rate);
        previous = current;
      }
    });
  }

  test('fourteen means +14 after one second, +28 after two, and +42 after three', () => {
    const counter = new StatsCounter(1000, 14, 0);
    expect(counter.read(1000)).toEqual({ total: 1014, rate: 14 });
    expect(counter.read(2000)).toEqual({ total: 1028, rate: 14 });
    expect(counter.read(3000)).toEqual({ total: 1042, rate: 14 });
  });

  test('missing fetches do not expire the last known rate, even after minutes', () => {
    const counter = new StatsCounter(1000, 14, 0);
    expect(counter.read(10_000)).toEqual({ total: 1140, rate: 14 });
    expect(counter.read(60_000)).toEqual({ total: 1840, rate: 14 });
    expect(counter.read(300_000)).toEqual({ total: 5200, rate: 14 });
  });

  test('unknown rates preserve the latest value and every elapsed second', () => {
    const counter = new StatsCounter(1000, 14, 0);
    for (let second = 2; second <= 60; second += 2) {
      counter.update(null, second * 1000);
      expect(counter.read(second * 1000)).toEqual({ total: 1000 + second * 14, rate: 14 });
    }
  });

  test('a missing current rate cannot replace the last known rate with an average of zero', () => {
    const counter = new StatsCounter(1000, 14, 0);
    counter.update(null, 2000, 0);
    expect(counter.read(3000)).toEqual({ total: 1042, rate: 14 });
  });

  test('a real zero stops immediately and missing data keeps zero until a new rate arrives', () => {
    const counter = new StatsCounter(1000, 14, 0);
    counter.update(0, 2500);
    expect(counter.read(2500)).toEqual({ total: 1035, rate: 0 });
    counter.update(null, 4000, 20);
    expect(counter.read(60_000)).toEqual({ total: 1035, rate: 0 });
    counter.update(3, 60_000);
    expect(counter.read(61_000)).toEqual({ total: 1038, rate: 3 });
  });

  test('rate changes finish elapsed time at the old rate without easing or losing fractions', () => {
    const counter = new StatsCounter(1000, 14, 0);
    counter.update(5, 1500);
    expect(counter.read(1500)).toEqual({ total: 1021, rate: 5 });
    expect(counter.read(2000)).toEqual({ total: 1023.5, rate: 5 });
    expect(counter.read(2500)).toEqual({ total: 1026, rate: 5 });
  });

  test('an unknown initial rate stays pending until a measurement exists', () => {
    const counter = new StatsCounter(1000, null, 0);
    expect(counter.read(1000)).toEqual({ total: 1000, rate: null });
    counter.update(null, 2000, 7);
    expect(counter.read(3000)).toEqual({ total: 1007, rate: 7 });
    counter.update(14, 3000);
    expect(counter.read(4000)).toEqual({ total: 1021, rate: 14 });
  });

  test('fractional event rates accumulate without per-frame rounding', () => {
    const counter = new StatsCounter(1000, 0.25, 0);
    for (let now = 0; now < 8000; now += 1000 / 60) counter.read(now);
    expect(counter.read(8000)).toEqual({ total: 1002, rate: 0.25 });
  });

  test('resuming a suspended tab counts the full elapsed time at the latest rate', () => {
    const counter = new StatsCounter(1000, 14, 0);
    counter.read(1000);
    counter.update(14, 120_000);
    expect(counter.read(121_000)).toEqual({ total: 2694, rate: 14 });
  });

  test('a reload seeds the server total instead of carrying over the old projection', () => {
    const beforeReload = new StatsCounter(1000, 14, 0);
    expect(beforeReload.read(60_000).total).toBe(1840);
    const afterReload = new StatsCounter(1500, 14, 0);
    expect(afterReload.read(0).total).toBe(1500);
    expect(afterReload.read(1000)).toEqual({ total: 1514, rate: 14 });
  });
});
