// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, mock, test } from 'bun:test';
import { stubSvelteKit } from './sveltekit';

stubSvelteKit();

const rpc = mock(async (subject: string) => (subject === 'rpc.modules.patch' ? { rev: 4, modules: [] } : { modules: [] }));
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({ ...nats, rpc }));

const { actions } = await import('../src/routes/(app)/loyalty/+page.server');

const locals = { locale: 'en', session: { user_id: '7' }, accountState: { value: { status: 'paid' } } };

test.each([
  { name: 'raffle', partial: '{"prizePoints":"5000"}', status: undefined, revision: 4 },
  { name: 'gamble', partial: '{"winPercent":"55"}', status: undefined, revision: 4 },
  { name: 'duel', partial: '{"minStake":"100"}', status: undefined, revision: 4 },
  { name: 'queue', partial: '{}', status: 400, revision: undefined },
  { name: 'gamble', partial: '{', status: 400, revision: undefined }
])('gameSettings scopes and validates $name patches', async ({ name, partial, status, revision }) => {
  const request = new Request('https://dashboard.test/loyalty?/gameSettings', {
    method: 'POST',
    body: new URLSearchParams({ name, partial, expected_rev: '3' })
  });
  const cookies = { set: () => {}, get: () => undefined, delete: () => {} };
  const url = new URL('https://dashboard.test/loyalty');
  const result = await actions.gameSettings({ request, locals, cookies, url } as never);
  if (status) {
    expect(result).toMatchObject({ status, data: { ok: false } });
  } else {
    expect(result).toEqual({ ok: true, conflict: false, rev: revision });
  }
});
