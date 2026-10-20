// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';

const recorded: string[] = [];
mock.module('newrelic', () => ({
  default: { recordMetric: (name: string) => recorded.push(name), noticeError: () => {} }
}));
const { newRelicMetrics } = await import('./cache-fabric');

test('cache metrics are named without the Custom/ prefix the agent adds itself', () => {
  newRelicMetrics('dashboard').event('hit', 'account:42');
  expect(recorded).toEqual(['Cache/dashboard/account/hit']);
});
