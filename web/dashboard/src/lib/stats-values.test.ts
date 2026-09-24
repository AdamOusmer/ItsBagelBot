// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { exactDisplay, formatStatTotal, validBoards, validStats } from './stats-values';

const stats = {
  messages_total: '9223372036854775807',
  events_total: '42',
  msg_rate: 60,
  event_rate: 60,
  msg_rate_now: 60,
  event_rate_now: 60,
  degraded: false
};

test('stats accept the exact limit and reject an imprecise total', () => {
  expect(validStats(stats)).toEqual(stats);
  expect(validStats({ ...stats, messages_total: '9223372036854775808' })).toBeNull();
});

test('boards reject an imprecise feed or channel count', () => {
  const boards = {
    channels: [{ id: '1', name: 'a', messages: '10', events: '10' }],
    feed: { total: '1', ranked: '1', entries: [{ id: '1', name: 'a', count: '1' }] },
    degraded: false
  };
  expect(validBoards(boards)).toEqual(boards);
  expect(validBoards({ ...boards, feed: { ...boards.feed, total: '9223372036854775808' } })).toBeNull();
  expect(validBoards({ ...boards, channels: [{ ...boards.channels[0], messages: -1 }] })).toBeNull();
});

test('animated totals stop at the exact display limit', () => {
  expect(exactDisplay(Number.MAX_SAFE_INTEGER + 60)).toBe(Number.MAX_SAFE_INTEGER);
  expect(exactDisplay(Number.POSITIVE_INFINITY)).toBe(0);
  expect(formatStatTotal('9223372036854775807', 0, 'en-US')).toBe('9,223,372,036,854,775,807');
});
