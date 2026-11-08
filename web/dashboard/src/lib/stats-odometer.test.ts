// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { StatsCounter } from './stats-odometer';

describe('stats counter playback', () => {
  test('the 3 msg/s preview adds three every second across updates', () => {
    const counter = new StatsCounter(1_500_000_000, 3, 0);
    let previous = counter.read(0).total;
    for (let second = 1; second <= 20; second++) {
      if (second % 2 === 0) counter.push(1_500_000_000 + second * 3, 3, second * 1000);
      const current = counter.read(second * 1000);
      expect(current.total - previous).toBe(3);
      expect(current.rate).toBe(3);
      previous = current.total;
    }
  });

  test.each([30, 60, 144])('14 msg/s adds fourteen in EVERY second at %i fps, across snapshot updates', (fps) => {
    const counter = new StatsCounter(1_500_000_000, 14, 0);
    let previous = counter.read(0).total;
    for (let second = 1; second <= 20; second++) {
      for (let frame = 1; frame <= fps; frame++) {
        const now = (second - 1) * 1000 + frame * 1000 / fps;
        if (frame === fps && second % 2 === 0) {
          counter.push(1_500_000_000 + second * 14, 14, now);
        }
        expect(counter.read(now).rate).toBe(14);
      }
      const current = counter.read(second * 1000).total;
      expect(current - previous).toBe(14);
      previous = current;
    }
  });

  test('source timestamps absorb arrival jitter and duplicate cached frames without changing speed', () => {
    const counter = new StatsCounter(10_000, 14, 0);
    counter.push(10_000, 14, 0); // same cached snapshot at connection open
    expect(counter.read(2200)).toEqual({ total: 10_002.8, rate: 14 });
    counter.push(10_028, 14, 2000); // arrives 200 ms late
    counter.push(10_028, 14, 2000); // cached replay, not another sample
    expect(counter.read(3000)).toEqual({ total: 10_014, rate: 14 });
    counter.push(10_056, 14, 4000); // arrives early enough for buffered playback
    counter.push(10_028, 14, 2000); // late older response cannot reset anything
    expect(counter.read(5000)).toEqual({ total: 10_042, rate: 14 });
  });

  test('rate changes play linearly, and the badge equals the actual growth in each interval', () => {
    const counter = new StatsCounter(1000, 14, 0);
    counter.push(1028, 14, 2000);
    counter.push(1068, 20, 4000);
    let previous = counter.read(4000).total;
    for (const now of [4500, 5000, 5500, 6000]) {
      const reading = counter.read(now);
      expect(reading.total - previous).toBe(10);
      expect(reading.rate).toBe(20);
      previous = reading.total;
    }
  });

  test('a five-second fallback poll keeps steady growth between responses', () => {
    const counter = new StatsCounter(1000, 14, 0);
    expect(counter.read(4000)).toEqual({ total: 1028, rate: 14 });
    counter.push(1070, 14, 5000);
    expect(counter.read(5000)).toEqual({ total: 1042, rate: 14 });
    expect(counter.read(9000)).toEqual({ total: 1098, rate: 14 });
    counter.push(1140, 14, 10_000);
    expect(counter.read(10_000)).toEqual({ total: 1112, rate: 14 });
  });

  test('a stalled or degraded stream stops projecting and reports zero growth', () => {
    const counter = new StatsCounter(1000, 14, 0);
    expect(counter.read(9000)).toEqual({ total: 1098, rate: 14 });
    expect(counter.read(10_000)).toEqual({ total: 1112, rate: 0 });
    counter.push(1000, 14, 0); // cached data does not extend freshness
    expect(counter.read(60_000)).toEqual({ total: 1112, rate: 0 });
    counter.push(1840, 14, 60_000);
    expect(counter.read(60_000)).toEqual({ total: 1812, rate: 14 });
  });

  test('zero and unknown rates do not invent growth', () => {
    const unknown = new StatsCounter(1000, null, 0);
    expect(unknown.read(1000)).toEqual({ total: 1000, rate: null });
    unknown.push(1028, 14, 2000);
    expect(unknown.read(3000)).toEqual({ total: 1014, rate: 14 });
    const idle = new StatsCounter(1000, 0, 0);
    idle.push(1000, 0, 2000);
    expect(idle.read(3000)).toEqual({ total: 1000, rate: 0 });
  });

  test('fractional event rates accumulate instead of disappearing in per-frame rounding', () => {
    const counter = new StatsCounter(1000, 0.25, 0);
    const start = counter.read(0).total;
    for (let now = 1000; now <= 8000; now += 1000) counter.read(now);
    expect(counter.read(8000).total - start).toBe(2);
  });

  test('late downward corrections hold without reporting a false positive rate', () => {
    const counter = new StatsCounter(1000, 14, 0);
    expect(counter.read(5000).total).toBe(1042);
    counter.push(1028, 0, 5000);
    expect(counter.read(5000)).toEqual({ total: 1042, rate: 0 });
    counter.push(1100, 14, 7000);
    expect(counter.read(8000).total).toBeGreaterThan(1042);
  });

  test('a genuine counter reset can lower the total and then resume', () => {
    const counter = new StatsCounter(1000, 14, 0);
    counter.read(2000);
    counter.push(10, 2, 2000);
    expect(counter.read(2000)).toEqual({ total: 10, rate: 0 });
    counter.push(14, 2, 4000);
    expect(counter.read(5000)).toEqual({ total: 12, rate: 2 });
  });

  test('resuming from an exact snapshot never rolls backward to refill the buffer', () => {
    const counter = new StatsCounter(1000, 14, 0, 1000);
    expect(counter.read(0)).toEqual({ total: 1000, rate: 0 });
    expect(counter.read(1000)).toEqual({ total: 1000, rate: 0 });
    counter.push(1028, 14, 2000);
    expect(counter.read(3000)).toEqual({ total: 1014, rate: 14 });
  });
});
