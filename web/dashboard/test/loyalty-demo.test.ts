// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { expect, test } from 'bun:test';
import { privateEnv, stubSvelteKit } from './sveltekit';

stubSvelteKit({ dev: true });
privateEnv.DEMO = '1';

const { load } = await import('../src/routes/(app)/loyalty/+page.server');
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
    games: catalogChildren('loyalty').map(({ id }) => ({ id, enabled: false }))
  });
});
