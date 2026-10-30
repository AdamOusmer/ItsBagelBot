// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { beforeEach, expect, mock, test } from 'bun:test';

const calls: { subject: string; payload: any; timeoutMs?: number }[] = [];
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

beforeEach(() => {
  calls.length = 0;
});

test('plain read sends no flags with the 2000ms timeout and tolerates no verification', async () => {
  reply = { found: true, channel };
  const r = await channelSubState('1001');
  expect(calls[0].payload).toEqual({ broadcaster_id: '1001' });
  expect(calls[0].timeoutMs).toBe(2000);
  expect(r).toEqual({ state: 'failing', error: 'boom', checkedAt: 't' });
});

test('verify sends the flag with the 8000ms timeout and returns the verification', async () => {
  const verification = { expected: 4, present: 2, missing: ['a', 'b'], repairing: false };
  reply = { found: true, channel, verification };
  const r = await channelSubState('1001', { verify: true });
  expect(calls[0].payload).toEqual({ broadcaster_id: '1001', verify: true });
  expect(calls[0].timeoutMs).toBe(8000);
  expect(r.verification).toEqual(verification);
});
