// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import assert from 'node:assert/strict';
import { describe, test } from 'node:test';
import { StatsCounter } from './stats-counter';

const STEP_MS = 20;

describe('stats counter motion', () => {
  for (const batchMs of [5000, 15_000]) {
    test(`sustains its speed through ${batchMs / 1000}s batches and zero-rate samples`, () => {
      const seed = 1_000_000;
      const speed = 100;
      const counter = new StatsCounter(seed, speed, 0);
      let previousTotal = seed;
      const speeds: number[] = [];
      for (let now = STEP_MS; now <= 120_000; now += STEP_MS) {
        if (now % 2000 === 0) {
          const total = seed + Math.floor(now / batchMs) * speed * batchMs / 1000;
          counter.sample(total, (total - previousTotal) / 2, now);
          previousTotal = total;
        }
        const before = counter.value;
        counter.advance(now, STEP_MS);
        assert.ok(counter.value >= before, 'totals never reverse between resets');
        if (now >= 60_000) speeds.push((counter.value - before) / (STEP_MS / 1000));
      }
      assert.ok(Math.min(...speeds) > speed * 0.9, 'no slowdown after a snapshot');
      assert.ok(Math.max(...speeds) < speed * 1.1, 'no catch-up burst after a batch');
      assert.ok(Math.abs(counter.value - (seed + 12_000)) < speed * batchMs / 1000,
        'the estimate stays within one batch of the true total');
    });
  }

  test('absorbs a total correction without changing speed abruptly', () => {
    const counter = new StatsCounter(1000, 100, 0);
    counter.sample(1600, 300, 2000);
    assert.equal(counter.value, 1000, 'arrival does not change the displayed total');
    assert.equal(counter.rate, 100, 'arrival does not change the displayed speed');
    counter.advance(2020, STEP_MS);
    assert.ok(counter.rate > 100 && counter.rate < 101);
    assert.ok(counter.value > 1000 && counter.value < 1003);
  });

  test('slows smoothly when snapshots stop, then stops extrapolating', () => {
    const counter = new StatsCounter(1000, 100, 0);
    for (let now = STEP_MS; now <= 10_000; now += STEP_MS) counter.advance(now, STEP_MS);
    const runningRate = counter.rate;
    counter.advance(10_020, STEP_MS);
    assert.ok(counter.rate < runningRate && counter.rate > runningRate * 0.99);
    for (let now = 10_040; now <= 30_000; now += STEP_MS) counter.advance(now, STEP_MS);
    const stoppedValue = counter.value;
    counter.advance(60_000, 30_000);
    assert.equal(counter.rate, 0);
    assert.equal(counter.value, stoppedValue);
  });

  test('a healthy but idle counter eventually stops', () => {
    const counter = new StatsCounter(1000, 100, 0);
    for (let now = STEP_MS; now <= 90_000; now += STEP_MS) {
      if (now % 2000 === 0) counter.sample(1000, 0, now);
      counter.advance(now, STEP_MS);
    }
    assert.ok(counter.rate < 0.001);
  });

  test('keeps rate deltas exact above the numeric animation limit', () => {
    const seed = 18_000_000_000_000_000_000n;
    const counter = new StatsCounter(String(seed), 10, 0);
    for (let now = STEP_MS; now <= 60_000; now += STEP_MS) {
      if (now % 2000 === 0) counter.sample(String(seed + BigInt(now / 100)), 10, now);
      counter.advance(now, STEP_MS);
    }
    assert.equal(counter.rate, 10);
    assert.equal(counter.value, Number.MAX_SAFE_INTEGER);
    counter.sample(String(seed - 1n), 0, 60_020);
    assert.equal(counter.rate, 0, 'a one-count reset is detected without rounding');
  });

  test('a real reset clears the old total and rate history', () => {
    const counter = new StatsCounter(1000, 100, 0);
    counter.sample(1200, 100, 2000);
    counter.sample(5, 0, 4000);
    assert.equal(counter.value, 5);
    assert.equal(counter.rate, 0);
    counter.sample(25, 10, 6000);
    counter.advance(6020, STEP_MS);
    assert.ok(counter.rate > 0 && counter.rate < 1);
  });

  test('a resumed frame cannot integrate an entire suspended interval', () => {
    const counter = new StatsCounter(1000, 100, 0);
    counter.sample(7000, 100, 60_000);
    counter.advance(60_000, 60_000);
    assert.ok(counter.value - 1000 < 30);
    counter.snap(60_000);
    assert.equal(counter.value, 7000);
  });
});
