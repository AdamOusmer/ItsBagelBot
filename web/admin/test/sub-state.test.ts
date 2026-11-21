// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { beforeEach, expect, mock, test } from 'bun:test';

const calls: { subject: string; payload: unknown; timeoutMs?: number }[] = [];
let reply: unknown;
mock.module('@bagel/kit/server/nats', () => ({
  rpc: mock(async (subject: string, payload: unknown, timeoutMs?: number) => {
    calls.push({ subject, payload, timeoutMs });
    return reply;
  }),
  publish: mock(async () => {}),
  subscribeDurable: mock(async () => {}),
  RpcError: class RpcError extends Error {}
}));

process.env.NEW_RELIC_ENABLED = 'false';
const { channelSubState } = await import('../src/lib/server/services');

const channel = { sub_state: 'failing', sub_error: 'boom', sub_checked_at: 't' };
const verification = { expected: 4, present: 2, missing: ['a', 'b'], repairing: false };
const mappedState = { state: 'failing', error: 'boom', checkedAt: 't' } as const;
const broadcasterId = '1001';
const subject = `${process.env.NATS_OUTGRESS_RPC_PREFIX || 'rpc.outgress'}.channel.get`;

beforeEach(() => {
  calls.length = 0;
});

test.each([
  {
    name: 'plain read uses the shorter timeout and accepts a reply without verification',
    options: undefined,
    reply: { found: true, channel },
    payload: { broadcaster_id: broadcasterId },
    timeoutMs: 2000,
    result: mappedState
  },
  {
    name: 'verified read sends the flag and returns verification',
    options: { verify: true },
    reply: { found: true, channel, verification },
    payload: { broadcaster_id: broadcasterId, verify: true },
    timeoutMs: 8000,
    result: { ...mappedState, verification }
  }
])('$name', async (scenario) => {
  reply = scenario.reply;
  const result = await channelSubState(broadcasterId, scenario.options);
  expect(calls).toEqual([{ subject, payload: scenario.payload, timeoutMs: scenario.timeoutMs }]);
  expect(result).toEqual(scenario.result);
});
