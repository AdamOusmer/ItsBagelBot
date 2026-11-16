// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, jest, mock, spyOn, test } from 'bun:test';

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
  { name: 'cold concurrent requests return without warming rates', initial: 'healthy', prewarm: 0 },
  { name: 'cached degraded init sample retries successfully', initial: 'degraded', prewarm: 0 },
  { name: 'rejected init sample retries successfully', initial: 'rejected', prewarm: 0 },
  { name: 'prewarmed requests return before the remaining span', initial: 'healthy', prewarm: 2 },
  { name: 'late requests retry a degraded sample without warming rates', initial: 'degraded', prewarm: 2 }
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

  const firstSampleAt = sampledAt + (row.initial === 'healthy' ? 0 : row.prewarm * 1000);
  for (let tick = 0; tick < row.prewarm; tick++) {
    sampledAt += 1000;
    jest.advanceTimersByTime(1000);
    await settle();
  }
  availability = 'healthy';
  let resolved = 0;
  const pending = Array.from({ length: 2 }, () => publicStats().then((stats) => {
    resolved++;
    return stats;
  }));
  await settle();
  expect(resolved).toBe(2);
  expect(reads).toBe(1 + row.prewarm + Number(row.initial !== 'healthy'));
  expect(await Promise.all(pending)).toEqual(Array(2).fill({
    messages_total: '1000', events_total: '2000', sampled_at: sampledAt,
    msg_rate: null, event_rate: null, msg_rate_now: null, event_rate_now: null, degraded: false
  }));

  for (const span of [4999, 5000]) {
    sampledAt = firstSampleAt + span;
    messages = 1000 + Math.floor(span / 10);
    jest.advanceTimersByTime(1000);
    await settle();
    const rate = span < 5000 ? null : 100;
    expect(await publicStats()).toEqual({
      messages_total: String(messages), events_total: String(messages * 2), sampled_at: sampledAt,
      msg_rate: rate, event_rate: rate === null ? null : rate * 2,
      msg_rate_now: rate, event_rate_now: rate === null ? null : rate * 2, degraded: false
    });
  }
  const readsBeforeRequest = reads;
  await publicStats();
  expect(reads).toBe(readsBeforeRequest);
});

test('samples without viewers using 10s now and 60s average windows', async () => {
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
    { at: 15_000, total: 200, now: 0, average: 200 / 15 },
    { at: 16_000, total: 230, now: 30 / 11, average: 230 / 16 },
    { at: 20_000, total: 350, now: 15, average: 17.5 },
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
