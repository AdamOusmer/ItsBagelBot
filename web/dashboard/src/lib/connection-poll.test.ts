// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { CONNECTION_POLL_FAST_MS, connectionPollDelay, connectionPollSettled, type ConnectionPollGoal } from './connection-poll';

test('polls quickly through the ordinary reconnect window, then backs off', () => {
  expect([0, 4999, 5000].map(connectionPollDelay)).toEqual([CONNECTION_POLL_FAST_MS, CONNECTION_POLL_FAST_MS, 2500]);
});

type Poll = { goal: ConnectionPollGoal; state: string; elapsedMs: number; sawUnsettled: boolean };

const settled: { name: string; poll: Poll; want: boolean }[] = [
  { name: 'connect waits for a transition or the grace period', poll: { goal: 'connected', state: 'ok', elapsedMs: 500, sawUnsettled: false }, want: false },
  { name: 'connect settles once a transition was seen', poll: { goal: 'connected', state: 'ok', elapsedMs: 500, sawUnsettled: true }, want: true },
  { name: 'connect settles after the short old-state grace period', poll: { goal: 'connected', state: 'ok', elapsedMs: 1500, sawUnsettled: false }, want: true },
  { name: 'connect never settles on a pending state', poll: { goal: 'connected', state: 'pending', elapsedMs: 5000, sawUnsettled: true }, want: false },
  { name: 'disconnect waits while the subscription is still ok', poll: { goal: 'disconnected', state: 'ok', elapsedMs: 5000, sawUnsettled: true }, want: false },
  { name: 'disconnect settles when unenrolled', poll: { goal: 'disconnected', state: 'unenrolled', elapsedMs: 500, sawUnsettled: false }, want: true },
  { name: 'disconnect settles when failing', poll: { goal: 'disconnected', state: 'failing', elapsedMs: 500, sawUnsettled: false }, want: true },
  { name: 'disconnect settles when revoked', poll: { goal: 'disconnected', state: 'revoked', elapsedMs: 500, sawUnsettled: false }, want: true }
];

test.each(settled)('connectionPollSettled $name', ({ poll, want }) => {
  expect(connectionPollSettled(poll.goal, poll.state, poll.elapsedMs, poll.sawUnsettled)).toBe(want);
});
