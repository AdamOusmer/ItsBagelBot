// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';

mock.module('$app/environment', () => ({ dev: false, browser: false, building: false }));

let reads = 0;
mock.module('./live-counters', () => ({
  liveTotals: async () => {
    reads++;
    return { messages_processed: String(100 * reads), events_processed: String(50 * reads) };
  }
}));

const { publicStats } = await import('./public-stats');

test('samples on its own timer so the rate windows fill without viewers', async () => {
  const first = await publicStats();
  expect(first.degraded).toBe(false);
  expect(first.messages_total).toBe('100');

  await new Promise((resolve) => setTimeout(resolve, 2300));
  const readsAfterIdle = reads;
  expect(readsAfterIdle).toBeGreaterThanOrEqual(3);

  const later = await publicStats();
  expect(later.msg_rate_now).toBeGreaterThan(0);
  expect(later.msg_rate).toBeGreaterThan(0);
  expect(reads).toBe(readsAfterIdle);
});
