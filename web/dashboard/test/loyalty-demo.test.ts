// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { privateEnv, stubSvelteKit } from './sveltekit';

stubSvelteKit({ dev: true });
privateEnv.DEMO = '1';

const { actions, load } = await import('../src/routes/(app)/loyalty/+page.server');
const { demoStandings } = await import('#lib/server/demo-data');
const { blankLoyaltyConfig, catalogChildren } = await import('@bagel/kit');

test('load supplies the loyalty demo shape without a freeze', async () => {
  const page = await load({ locals: { locale: 'en' } } as never);
  expect(page).toEqual({
    enabled: true,
    config: blankLoyaltyConfig(),
    top: demoStandings(),
    topFreeze: null,
    freeze: null,
    raffle: null,
    games: catalogChildren('loyalty').map(({ id }) => ({ id, enabled: false, config: {}, revision: 0 }))
  });
});

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
  const result = await actions.gameSettings({ request, locals: { locale: 'en' } } as never);
  if (status) {
    expect(result).toMatchObject({ status, data: { ok: false } });
  } else {
    expect(result).toEqual({ ok: true, conflict: false, rev: revision });
  }
});
