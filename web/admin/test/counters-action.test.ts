// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { expect, mock, test } from 'bun:test';
import { registerServerConfig } from '@bagel/kit/server/config';
import { stubSvelteKit } from './sveltekit';

stubSvelteKit();
registerServerConfig({ cacheInvalidationPrefix: 'inv' });
mock.module('virtual:bagel-i18n', () => ({ urls: {}, loaders: {}, sliced: false }));
mock.module('@bagel/kit/i18n', () => ({ translate: (_locale: string, key: string) => key }));

const admin = { id: '1', login: 'mod', display_name: 'Mod', role: 'admin' };
mock.module('#lib/server/access', () => ({ allows: () => true, requireAdmin: async () => admin, requireRole: async () => admin }));
mock.module('#lib/server/audit', () => ({ audit: () => {} }));

let rpcReply: Record<string, unknown> | Error = {};
let rpcRequest: unknown;
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  publish: async () => {},
  rpc: async (_subject: string, request: unknown) => {
    rpcRequest = request;
    if (rpcReply instanceof Error) throw rpcReply;
    return rpcReply;
  }
}));

const { actions } = await import('../src/routes/(admin)/counters/+page.server');

function submitCounter(action: 'create' | 'set', fields: Record<string, string>): Promise<unknown> {
  const body = new FormData();
  for (const [name, value] of Object.entries(fields)) body.set(name, value);
  const event = {
    request: new Request(`https://admin.test/counters?/${action}`, { method: 'POST', body }),
    locals: { session: { user_id: '1' }, locale: 'en' }
  };
  return (actions[action] as (e: typeof event) => Promise<unknown>)(event);
}

test.each([
  { name: 'a counter another session deleted', reply: { found: false } as Record<string, unknown> | Error, status: 404 },
  { name: 'an RPC timeout', reply: new Error('TIMEOUT'), status: 502 }
])('setting $name answers $status', async ({ reply, status }) => {
  rpcReply = reply;
  expect(await submitCounter('set', { name: 'feeds', value: '5' })).toMatchObject({ status, data: { ok: false } });
});

test('creating a counter returns the persisted row, including an existing 64-bit value', async () => {
  const counter = { name: 'feeds', scope: 'bot', value: '9223372036854775807' };
  rpcReply = { counter };
  expect(await submitCounter('create', { name: ' !Feeds ' })).toEqual({ ok: true, counter });
  expect(rpcRequest).toEqual({ user_id: '0', name: 'feeds', scope: 'bot' });
});

test.each([
  { input: '0005', canonical: '5' },
  { input: '9223372036854775807', canonical: '9223372036854775807' }
])('setting $input sends the decimal string $canonical', async ({ input, canonical }) => {
  rpcReply = { found: true };
  expect(await submitCounter('set', { name: 'feeds', value: input })).toEqual({ ok: true });
  expect(rpcRequest).toEqual({ user_id: '0', name: 'feeds', counter_value: canonical });
});
