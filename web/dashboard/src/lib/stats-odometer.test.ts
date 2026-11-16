// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, test } from 'bun:test';
import { StatsCounter } from './stats-odometer';

function outageSample(now: number) {
  return {
    healthy: now < 20_000 || now >= 80_000,
    rate: now < 25_000 ? 12 : 4,
    total: 1000 + 12 * Math.min(now, 25_000) / 1000 + 4 * Math.max(0, now - 25_000) / 1000
  };
}

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

  test.each([
    { rate: 14, times: [1000, 2000, 3000] },
    { rate: 14, times: [10_000, 60_000, 300_000] },
    { rate: 0.25, times: [2000, 4000, 8000] }
  ])('bounds prediction at $rate per second without fetches or rounding', ({ rate, times }) => {
    const counter = new StatsCounter(1000, rate, 0);
    for (const now of times) {
      expect(counter.read(now)).toEqual({ total: 1000 + rate * Math.min(now, 3000) / 1000, rate });
    }
  });

  test.each([
    { initial: 14, fallback: 0, expected: 14 },
    { initial: 0, fallback: 20, expected: 0 },
    { initial: null, fallback: 7, expected: 7 },
    { initial: null, fallback: null, expected: null }
  ])('keeps the last known rate $initial through null polls with fallback $fallback', ({ initial, fallback, expected }) => {
    const counter = new StatsCounter(1000, initial, 0);
    counter.update(null, 0, fallback);
    for (let second = 2; second <= 60; second += 2) {
      counter.update(null, second * 1000, fallback);
      expect(counter.read(second * 1000)).toEqual({ total: 1000 + Math.min(second, 3) * (expected ?? 0), rate: expected });
    }
  });
});

describe('stats counter smoothing and truth', () => {
  test.each([
    { from: 14, to: 28, fps: 30 },
    { from: 28, to: 7, fps: 60 },
    { from: 14, to: 0, fps: 144 }
  ])('smooths $from to $to at $fps fps without overshooting', ({ from, to, fps }) => {
    const counter = new StatsCounter(1000, from, 0);
    counter.update(to, 0);
    let previous: number = from;
    for (let frame = 0; frame <= fps * 6; frame++) {
      const now = frame * 1000 / fps;
      const reading = counter.read(now);
      expect(reading.rate).toBeCloseTo(to + (from - to) * Math.exp(-now / 2000), 10);
      expect(Math.abs(reading.rate! - to)).toBeLessThanOrEqual(Math.abs(previous - to));
      previous = reading.rate!;
    }
    expect(Math.abs(previous - to)).toBeLessThan(Math.abs(from - to) * 0.05);
  });

  test.each([30, 60, 144])('integrates the displayed exponential rate independently of %i fps', (fps) => {
    const counter = new StatsCounter(1000, 14, 0);
    counter.update(28, 0);
    for (let frame = 0; frame <= fps * 6; frame++) {
      const now = frame * 1000 / fps;
      if (frame % (fps * 2) === 0) counter.update(28, now);
      counter.read(now);
    }
    expect(counter.read(6000).total).toBeCloseTo(1000 + 28 * 6 - 28 * (1 - Math.exp(-3)), 8);
  });

  test.each([900, 1100])('converges from %i to projected server truth without decreasing', (initial) => {
    const counter = new StatsCounter(initial, 14, 0);
    counter.update(14, 0, null, { total: 1000, sampledAt: -500 });
    let previous: number = initial;
    for (let now = 0; now <= 32_000; now += 100) {
      if (now % 1000 === 0) counter.update(14, now, null, { total: 1000 + 14 * now / 1000, sampledAt: now - 500 });
      const reading = counter.read(now);
      expect(reading.total).toBeGreaterThanOrEqual(previous);
      previous = reading.total;
    }
    expect(previous).toBeCloseTo(1007 + 14 * 32, 0);
  });

  test.each([
    { initial: 1000, truth: 1800, at: 1000, sampledAt: 500, expected: 1807 },
    { initial: 1000, truth: 900, at: 1000, sampledAt: 1000, expected: 900 },
    { initial: 1000, truth: 900, at: 600_000, sampledAt: 0, expected: 942 },
    { initial: 1000, truth: 2800, at: 120_000, sampledAt: 119_500, expected: 2807 }
  ])('snaps stale seeds and resumed tabs to $truth with a fresh rate', ({ initial, truth, at, sampledAt, expected }) => {
    const counter = new StatsCounter(initial, 3, 0);
    counter.update(14, at, null, { total: truth, sampledAt, snap: true });
    expect(counter.read(at)).toEqual({ total: expected, rate: 14 });
  });

  test.each([
    { truth: false, gap: 60_000 },
    { truth: true, gap: 60_000 },
    { truth: false, gap: 600_000 },
    { truth: true, gap: 600_000 }
  ])('bounds lead across a $gap ms hidden gap, server truth $truth', ({ truth, gap }) => {
    const counter = new StatsCounter(1000, 12, 0);
    if (truth) counter.update(12, 0, null, { total: 1000, sampledAt: 0, snap: true });
    const stopped = counter.read(30_000).total;
    const resumed = counter.read(gap).total;
    expect(resumed - 1000).toBeLessThanOrEqual(36);
    expect(resumed).toBeCloseTo(stopped, 5);
    counter.update(4, gap, null, { total: 1000 + 4 * gap / 1000, sampledAt: gap, snap: true });
    expect(counter.read(gap).total).toBe(1000 + 4 * gap / 1000);
  });

  test.each([30, 60, 144])('recovers from a 60s degraded outage without a long freeze at %i fps', (fps) => {
    const counter = new StatsCounter(1000, 12, 0);
    let previous = 1000;
    let unchangedAt = 80_000;
    let longestFreeze = 0;
    for (let frame = 0; frame <= fps * 140; frame++) {
      const now = frame * 1000 / fps;
      const sample = outageSample(now);
      if (sample.healthy && frame % fps === 0) {
        counter.update(sample.rate, now, null, { total: sample.total, sampledAt: now });
      }
      const shown = Math.round(counter.read(now).total);
      expect(shown).toBeGreaterThanOrEqual(previous);
      if (now === 80_000) unchangedAt = now;
      if (shown !== previous) unchangedAt = now;
      if (now >= 80_000) longestFreeze = Math.max(longestFreeze, now - unchangedAt);
      previous = shown;
    }
    expect(longestFreeze).toBeLessThan(1000);
    expect(previous).toBeCloseTo(1760, 0);
  });

  test.each([null, 14])('tracks totals through a null poll with previous rate %s', (rate) => {
    const counter = new StatsCounter(1000, rate, 0);
    counter.update(null, 1000, 14, { total: 1014, sampledAt: 1000, snap: true });
    counter.update(null, 2000, 0, { total: 1028, sampledAt: 2000 });
    expect(counter.read(3000)).toEqual({ total: 1042, rate: 14 });
  });

  test.each([false, true])('continues a changed target from the displayed rate, reduced motion %s', (reduced) => {
    const counter = new StatsCounter(1000, 14, 0);
    counter.update(28, 0);
    const before = counter.read(500, { animateRate: !reduced });
    counter.update(7, 500);
    const after = counter.read(500, { animateRate: !reduced });
    expect(after).toEqual({ total: before.total, rate: reduced ? 7 : before.rate });
  });

  test.each([250, 500, 750])('unchanged and null samples at %i ms preserve the smoothing clock', (at) => {
    const counter = new StatsCounter(1000, 14, 0);
    counter.update(28, 0);
    counter.update(null, at, 0);
    counter.update(28, at);
    expect(counter.read(1000).rate).toBeCloseTo(28 - 14 * Math.exp(-0.5), 10);
  });

  test.each([250, 1000])('switching to reduced motion at %i ms preserves totals and does not replay smoothing', (at) => {
    const counter = new StatsCounter(1000, 14, 0);
    counter.update(28, 0);
    const before = counter.read(at);
    expect(counter.read(at, { animateRate: false })).toEqual({ total: before.total, rate: 28 });
    expect(counter.read(at + 1000)).toEqual({ total: before.total + 28, rate: 28 });
  });
});
