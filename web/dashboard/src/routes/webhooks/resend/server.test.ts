// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { createHmac } from 'node:crypto';
import { beforeEach, expect, test } from 'bun:test';
import { privateEnv, stubSvelteKit } from '../../../../test/sveltekit';
import { installMarketingRpc, resetMarketingRpc, rpcCalls, rpcState } from '../../../../test/marketing-rpc';

stubSvelteKit();
await installMarketingRpc();
const { POST } = await import('./+server');

const key = Buffer.alloc(24, 5);
const secret = `whsec_${key.toString('base64')}`;
const suppress = 'rpc.internal.users.marketing.suppress';
const delivery = 'rpc.internal.users.marketing.delivery';
const tagged = { category: 'marketing' };

type Overrides = { id?: string; timestamp?: number; signature?: string; omit?: string };

function sign(id: string, timestamp: number, body: string): string {
  return `v1,${createHmac('sha256', key).update(`${id}.${timestamp}.${body}`).digest('base64')}`;
}

function deliver(payload: unknown, { id = 'msg_1', timestamp = Math.floor(Date.now() / 1000), signature, omit }: Overrides = {}) {
  const body = JSON.stringify(payload);
  const headers: Record<string, string> = {
    'content-type': 'application/json',
    'svix-id': id,
    'svix-timestamp': String(timestamp),
    'svix-signature': signature ?? sign(id, timestamp, body)
  };
  if (omit) delete headers[omit];
  return POST({ request: new Request('https://dashboard.itsbagelbot.com/webhooks/resend', { method: 'POST', headers, body }) } as never);
}

function event(type: string, data: Record<string, unknown>) {
  return { type, data: { email_id: 're_1', to: ['viewer@example.test'], ...data } };
}

beforeEach(() => {
  resetMarketingRpc();
  privateEnv.RESEND_WEBHOOK_SECRET = secret;
});

test('an unset secret refuses every delivery', async () => {
  privateEnv.RESEND_WEBHOOK_SECRET = undefined;
  expect((await deliver(event('email.complained', {}))).status).toBe(503);
  expect(rpcCalls).toEqual([]);
});

test.each([
  { name: 'a bad signature', overrides: { signature: 'v1,AAAA' } },
  { name: 'a signature for another version', overrides: { signature: sign('msg_1', 0, '{}').replace('v1', 'v2') } },
  { name: 'a stale timestamp', overrides: { timestamp: Math.floor(Date.now() / 1000) - 600 } },
  { name: 'a future timestamp', overrides: { timestamp: Math.floor(Date.now() / 1000) + 600 } },
  { name: 'a missing svix-id', overrides: { omit: 'svix-id' } },
  { name: 'a missing signature', overrides: { omit: 'svix-signature' } }
])('rejects $name with 401 and no Users call', async ({ overrides }) => {
  expect((await deliver(event('email.complained', { tags: tagged }), overrides)).status).toBe(401);
  expect(rpcCalls).toEqual([]);
});

test('accepts the current signature among rotated ones', async () => {
  const payload = event('email.complained', {});
  const body = JSON.stringify(payload);
  const timestamp = Math.floor(Date.now() / 1000);
  const signature = `v1,AAAA ${sign('msg_1', timestamp, body)}`;
  expect((await deliver(payload, { timestamp, signature })).status).toBe(200);
  expect(rpcCalls.map(([subject]) => subject)).toEqual([suppress]);
});

test.each([
  { name: 'a complaint', type: 'email.complained', data: {}, want: [[suppress, { email: 'viewer@example.test', reason: 'complaint' }]] },
  {
    name: 'a tagged complaint',
    type: 'email.complained',
    data: { tags: tagged },
    want: [[suppress, { email: 'viewer@example.test', reason: 'complaint' }], [delivery, { resend_id: 're_1', event: 'complained' }]]
  },
  {
    name: 'a tagged permanent bounce',
    type: 'email.bounced',
    data: { tags: [{ name: 'category', value: 'marketing' }], to: ['Viewer <viewer@example.test>'], bounce: { type: 'Permanent' } },
    want: [[suppress, { email: 'viewer@example.test', reason: 'hard_bounce' }], [delivery, { resend_id: 're_1', event: 'bounced' }]]
  },
  { name: 'an untagged transient bounce', type: 'email.bounced', data: { bounce: { type: 'Transient' } }, want: [] },
  { name: 'a tagged transient bounce', type: 'email.bounced', data: { tags: tagged, bounce: { type: 'Transient' } }, want: [[delivery, { resend_id: 're_1', event: 'bounced' }]] },
  { name: 'a suppression notice', type: 'email.suppressed', data: {}, want: [[suppress, { email: 'viewer@example.test', reason: 'hard_bounce' }]] },
  { name: 'a tagged delivery', type: 'email.delivered', data: { tags: tagged }, want: [[delivery, { resend_id: 're_1', event: 'delivered' }]] },
  { name: 'an untagged delivery', type: 'email.delivered', data: {}, want: [] },
  { name: 'an unrelated event', type: 'email.opened', data: { tags: tagged }, want: [] }
])('maps $name to Users calls', async ({ type, data, want }) => {
  expect((await deliver(event(type, data))).status).toBe(200);
  expect(rpcCalls).toEqual(want as unknown as typeof rpcCalls);
});

test('answers 500 when Users fails so Resend retries', async () => {
  rpcState.refusal = 'unavailable';
  expect((await deliver(event('email.complained', {}))).status).toBe(500);
});

test.each([
  { name: 'invalid JSON', body: '{', status: 400 },
  { name: 'a payload without a type', body: '{"data":{}}', status: 400 },
  { name: 'an oversized payload', body: JSON.stringify({ type: 'email.opened', pad: 'x'.repeat(70_000) }), status: 413 }
])('answers $status for $name once signed', async ({ body, status }) => {
  const timestamp = Math.floor(Date.now() / 1000);
  const headers = { 'svix-id': 'msg_2', 'svix-timestamp': String(timestamp), 'svix-signature': sign('msg_2', timestamp, body) };
  const request = new Request('https://dashboard.itsbagelbot.com/webhooks/resend', { method: 'POST', headers, body });
  expect((await POST({ request } as never)).status).toBe(status);
  expect(rpcCalls).toEqual([]);
});
