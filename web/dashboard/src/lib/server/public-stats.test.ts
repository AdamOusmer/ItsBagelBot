// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, jest, mock, spyOn, test } from 'bun:test';
import type { PublicStats } from './public-stats';

mock.module('$app/env', () => ({ dev: false, browser: false, building: false }));

let reads = 0;
let messages = 0;
let availability = 'healthy';
mock.module('./live-counters', () => ({
  liveTotals: async () => {
    reads++;
    if (availability === 'rejected') throw new Error('not connected');
    if (availability === 'degraded') return null;
    return { messages_processed: String(messages), events_processed: String(messages * 2) };
  }
}));

async function settle(): Promise<void> {
  for (let i = 0; i < 10; i++) await Promise.resolve();
}

function loadSampler(scenario: string): Promise<typeof import('./public-stats')> {
  return import(`./public-stats?${scenario}`);
}

afterEach(() => {
  jest.clearAllTimers();
  jest.useRealTimers();
  mock.restore();
});

const coldCases = [
  { name: 'cold concurrent requests wait for a 5s span', initial: 'healthy', prewarm: 0, cap: false },
  { name: 'cached degraded init sample retries successfully', initial: 'degraded', prewarm: 0, cap: false },
  { name: 'rejected init sample retries successfully', initial: 'rejected', prewarm: 0, cap: false },
  { name: 'prewarmed requests wait only for the remaining span', initial: 'healthy', prewarm: 2, cap: false },
  { name: 'cap returns null rates 6s after the first successful sample', initial: 'degraded', prewarm: 2, cap: true },
  { name: 'late requests share the cap started by init', initial: 'healthy', prewarm: 2, cap: true }
];

test.each(coldCases)('$name', async (row) => {
  jest.useFakeTimers();
  reads = 0;
  messages = 1000;
  availability = row.initial;
  let sampledAt = 10_000;
  spyOn(Date, 'now').mockImplementation(() => sampledAt);
  const { publicStats, startPublicStats } = await loadSampler(row.name);
  startPublicStats();
  startPublicStats();
  await settle();
  expect(reads).toBe(1);

  jest.advanceTimersByTime(row.prewarm * 1000);
  await settle();
  availability = 'healthy';
  const firstSampleAt = sampledAt;
  let resolved = 0;
  const pending = Array.from({ length: 2 }, () => publicStats().then((stats: PublicStats) => {
    resolved++;
    return stats;
  }));
  await settle();
  expect(reads).toBe(1 + Number(row.prewarm > 0) + Number(row.initial !== 'healthy'));

  const allSpans = row.cap ? [1000, 2000, 3000, 4000, 4999, 4999] : [1000, 2000, 3000, 4999, 5000];
  const spans = row.initial === 'healthy' ? allSpans.slice(row.prewarm) : allSpans;
  for (const span of spans) {
    expect(resolved).toBe(0);
    sampledAt = firstSampleAt + span;
    messages = 1000 + Math.floor(span / 10);
    jest.advanceTimersByTime(1000);
    await settle();
  }
  const results = await Promise.all(pending);
  const rate = row.cap ? null : 100;
  expect(results).toEqual(Array(2).fill({
    messages_total: String(messages), events_total: String(messages * 2), sampled_at: sampledAt,
    msg_rate: rate, event_rate: rate === null ? null : rate * 2,
    msg_rate_now: rate, event_rate_now: rate === null ? null : rate * 2, degraded: false
  }));
  const readsAfterWarmup = reads;
  await publicStats();
  expect(reads).toBe(readsAfterWarmup);
});

test('samples without viewers using 15s now and 60s average windows', async () => {
  jest.useFakeTimers();
  reads = 0;
  messages = 0;
  availability = 'healthy';
  let sampledAt = 0;
  spyOn(Date, 'now').mockImplementation(() => sampledAt);
  const { publicStats, startPublicStats } = await loadSampler('windows');
  startPublicStats();
  await settle();

  sampledAt = 1000;
  messages = 200;
  jest.advanceTimersByTime(1000);
  await settle();

  const samples = [
    { at: 5000, total: 200, now: 40, average: 40 },
    { at: 10_000, total: 200, now: 20, average: 20 },
    { at: 15_000, total: 200, now: 200 / 15, average: 200 / 15 },
    { at: 16_000, total: 230, now: 2, average: 230 / 16 },
    { at: 20_000, total: 350, now: 10, average: 17.5 },
    { at: 60_000, total: 1550, now: 30, average: 1550 / 60 }
  ];
  for (const row of samples) {
    sampledAt = row.at;
    messages = row.total;
    jest.advanceTimersByTime(1000);
    await settle();
    expect(await publicStats()).toEqual({
      messages_total: String(row.total), events_total: String(row.total * 2), sampled_at: row.at,
      msg_rate_now: row.now, event_rate_now: row.now * 2,
      msg_rate: row.average, event_rate: row.average * 2, degraded: false
    });
  }
});
