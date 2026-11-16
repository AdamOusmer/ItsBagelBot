// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, expect, jest, mock, spyOn, test } from 'bun:test';

mock.module('$app/env', () => ({ dev: false, browser: false, building: false }));

type Availability = 'healthy' | 'rejected' | 'degraded' | 'pending';
type Step = { at: number; status: number; availability?: Availability; nats?: boolean; probe?: boolean };
type Scenario = { name: string; availability: Availability; messages?: number; steps: Step[] };

let availability: Availability = 'healthy';
let messages = 0;
let natsReady = true;
let sampler: typeof import('../../lib/server/public-stats');

mock.module('../../lib/server/live-counters', () => ({
  liveTotals: async () => {
    if (availability === 'rejected') throw new Error('not connected');
    if (availability === 'degraded') return null;
    if (availability === 'pending') return new Promise<never>(() => {});
    return { messages_processed: String(messages), events_processed: String(messages * 2) };
  }
}));
mock.module('@bagel/kit/server/nats', () => ({ ready: async () => natsReady }));
mock.module('@bagel/kit/server/valkey-store', () => ({ ready: async () => false }));
mock.module('@bagel/kit/server/rate-limit', () => ({ rateLimiterReady: async () => false }));
mock.module('#lib/server/public-stats', () => ({ publicStatsWarm: () => sampler.publicStatsWarm() }));

const { GET } = await import('./+server');

async function settle(): Promise<void> {
  for (let i = 0; i < 10; i++) await Promise.resolve();
}

afterEach(() => {
  jest.clearAllTimers();
  jest.useRealTimers();
  mock.restore();
});

const capSteps: Step[] = [
  { at: 0, status: 503 },
  { at: 4999, status: 503 },
  { at: 7999, status: 503 },
  { at: 8000, status: 200 },
  { at: 9000, status: 200 }
];
const rateSteps: Step[] = [
  { at: 0, status: 503 },
  { at: 4999, status: 503 },
  { at: 5000, status: 200 },
  { at: 6000, status: 200, availability: 'rejected' },
  { at: 7000, status: 200, availability: 'degraded' }
];
const cases: Scenario[] = [
  { name: 'a healthy rate opens readiness permanently', availability: 'healthy', messages: 100, steps: rateSteps },
  { name: 'a healthy zero rate opens readiness permanently', availability: 'healthy', steps: rateSteps },
  { name: 'rejected Valkey reads cannot extend the startup cap', availability: 'rejected', steps: capSteps },
  { name: 'degraded Valkey reads cannot extend the startup cap', availability: 'degraded', steps: capSteps },
  { name: 'an unfinished stats read cannot extend the startup cap', availability: 'pending', steps: capSteps },
  { name: 'a rate latches before the next readiness probe', availability: 'healthy', steps: [
    { at: 5000, status: 200, probe: false }, { at: 6000, status: 200, availability: 'rejected' }
  ] },
  { name: 'NATS still gates a warm replica', availability: 'rejected', steps: [
    { at: 8000, status: 503, nats: false }, { at: 9000, status: 200, nats: true }
  ] }
];

test.each(cases)('$name', async (row) => {
  jest.useFakeTimers();
  availability = row.availability;
  messages = 0;
  natsReady = true;
  let now = 0;
  spyOn(Date, 'now').mockImplementation(() => now);
  sampler = await import(`../../lib/server/public-stats?readyz-${row.name}`);
  sampler.startPublicStats();
  await settle();

  for (const step of row.steps) {
    now = step.at;
    messages = row.messages ?? 0;
    availability = step.availability ?? row.availability;
    natsReady = step.nats ?? true;
    sampler.startPublicStats();
    jest.advanceTimersByTime(1000);
    await settle();
    if (step.probe === false) continue;
    const response = await GET({} as never);
    expect({ at: now, status: response.status, body: await response.text(), cache: response.headers.get('cache-control') })
      .toEqual({ at: now, status: step.status, body: step.status === 200 ? 'ok' : 'not ready', cache: 'no-store' });
  }
});
