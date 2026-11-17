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

let setReply: Record<string, unknown> | Error = {};
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  publish: async () => {},
  rpc: async () => {
    if (setReply instanceof Error) throw setReply;
    return setReply;
  }
}));

const { actions } = await import('../src/routes/(admin)/counters/+page.server');

function setCounter(): Promise<unknown> {
  const body = new FormData();
  body.set('name', 'feeds');
  body.set('value', '5');
  const event = {
    request: new Request('https://admin.test/counters?/set', { method: 'POST', body }),
    locals: { session: { user_id: '1' }, locale: 'en' }
  };
  return (actions.set as (e: typeof event) => Promise<unknown>)(event);
}

test.each([
  { name: 'a counter another session deleted', reply: { found: false } as Record<string, unknown> | Error, status: 404 },
  { name: 'an RPC timeout', reply: new Error('TIMEOUT'), status: 502 }
])('setting $name answers $status', async ({ reply, status }) => {
  setReply = reply;
  expect(await setCounter()).toMatchObject({ status, data: { ok: false } });
});
