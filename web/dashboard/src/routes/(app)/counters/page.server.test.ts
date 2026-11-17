// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();

let reply: Record<string, unknown> | Error = {};
const realNats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...realNats,
  publish: async () => {},
  rpc: async () => {
    if (reply instanceof Error) throw reply;
    return reply;
  }
}));

const { actions } = await import('./+page.server');

function renameCounter(): Promise<unknown> {
  const body = new FormData();
  body.set('name', 'deaths');
  body.set('new_name', 'falls');
  const event = {
    request: new Request('https://dashboard.test/counters?/rename', { method: 'POST', body }),
    locals: { session: { user_id: '7' }, locale: 'en' } as unknown as App.Locals
  };
  return (actions.rename as (e: typeof event) => Promise<unknown>)(event);
}

test.each([
  { name: 'a counter another session removed', reply: { found: false } as Record<string, unknown> | Error, status: 404 },
  { name: 'an RPC timeout', reply: new Error('TIMEOUT'), status: 502 }
])('renaming $name answers $status', async ({ reply: next, status }) => {
  reply = next;
  expect(await renameCounter()).toMatchObject({ status, data: { ok: false } });
});
