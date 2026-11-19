// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();

let editReply: Record<string, unknown> | Error = {};
const realNats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...realNats,
  publish: async () => {},
  rpc: async () => {
    if (editReply instanceof Error) throw editReply;
    return editReply;
  }
}));

const { actions } = await import('./+page.server');

function editQuote(): Promise<unknown> {
  const body = new FormData();
  for (const [name, value] of Object.entries({ number: '3', text: 'still here', quote_date: '2026-11-01' })) body.set(name, value);
  const event = {
    request: new Request('https://dashboard.test/quotes?/edit', { method: 'POST', body }),
    locals: { session: { user_id: '7' }, locale: 'en' } as unknown as App.Locals
  };
  return (actions.edit as (e: typeof event) => Promise<unknown>)(event);
}

test.each([
  { name: 'a quote another session deleted', reply: { found: false } as Record<string, unknown> | Error, status: 404 },
  { name: 'an RPC timeout', reply: new Error('TIMEOUT'), status: 502 }
])('editing $name answers $status', async ({ reply, status }) => {
  editReply = reply;
  expect(await editQuote()).toMatchObject({ status, data: { ok: false } });
});
