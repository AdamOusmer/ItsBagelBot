// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { CONNECTION_POLL_FAST_MS, connectionPollDelay, connectionPollSettled, type ConnectionPollGoal } from './connection-poll';

test('polls quickly through the ordinary reconnect window, then backs off', () => {
  expect([0, 4999, 5000].map(connectionPollDelay)).toEqual([CONNECTION_POLL_FAST_MS, CONNECTION_POLL_FAST_MS, 2500]);
});

type Poll = [state: string, elapsedMs: number, sawUnsettled: boolean];

const settledFor = (goal: ConnectionPollGoal, polls: Poll[]): boolean[] =>
  polls.map(([state, elapsedMs, sawUnsettled]) => connectionPollSettled(goal, state, elapsedMs, sawUnsettled));

test('connect waits for a transition or a short old-state grace period', () => {
  expect(
    settledFor('connected', [
      ['ok', 500, false],
      ['ok', 500, true],
      ['ok', 1500, false],
      ['pending', 5000, true]
    ])
  ).toEqual([false, true, true, false]);
});

test('disconnect waits for an inactive subscription state', () => {
  expect(
    settledFor('disconnected', [
      ['ok', 5000, true],
      ['unenrolled', 500, false],
      ['failing', 500, false],
      ['revoked', 500, false]
    ])
  ).toEqual([false, true, true, true]);
});
