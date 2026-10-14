// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';

process.env.NEW_RELIC_ENABLED = 'false';
mock.module('$app/environment', () => ({ dev: false }));

const calls: Array<{ subject: string; body: any; timeoutMs?: number }> = [];
let reply: unknown;
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string, body: any, timeoutMs?: number) => {
    calls.push({ subject, body, timeoutMs });
    return reply;
  }
}));
const { channelSubState } = await import('./services');

const channel = { sub_state: 'ok', sub_error: '', sub_checked_at: 't' };

beforeEach(() => {
  calls.length = 0;
});

test('plain read sends no flags and keeps the 2000ms timeout', async () => {
  reply = { found: true, channel };
  const r = await channelSubState('1001');
  expect(calls[0].body).toEqual({ broadcaster_id: '1001' });
  expect(calls[0].timeoutMs).toBe(2000);
  expect(r).toEqual({ state: 'ok', error: '', checkedAt: 't' });
});

test('verify and repair pass through with the 8000ms timeout', async () => {
  const verification = { expected: 4, present: 3, missing: ['channel.chat.message'], repairing: true };
  reply = { found: true, channel: { ...channel, sub_state: 'pending' }, verification };
  const r = await channelSubState('1001', { verify: true, repair: true });
  expect(calls[0].body).toEqual({ broadcaster_id: '1001', verify: true, repair: true });
  expect(calls[0].timeoutMs).toBe(8000);
  expect(r.state).toBe('pending');
  expect(r.verification).toEqual(verification);
});

test('unrecognized state stays unknown and keeps the verification', async () => {
  const verification = { expected: 4, present: 4, repairing: false, throttled: true };
  reply = { found: true, channel: { ...channel, sub_state: '' }, verification };
  const r = await channelSubState('1001', { verify: true });
  expect(r.state).toBe('unknown');
  expect(r.verification).toEqual(verification);
});
