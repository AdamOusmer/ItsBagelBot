// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const purged: string[] = [];
mock.module('./edge-purge', () => ({ schedulePurgeChannel: (userId: string) => void purged.push(userId) }));

const modules = [{ name: 'loyalty', is_enabled: true, revision: 4, configs: {} }];
const nats = await import('@bagel/kit/server/nats');
const { SUB } = await import('./services');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string) => {
    if (subject === `${SUB.modules}.upsert`) return { modules };
    if (subject === `${SUB.projector}.modules.replace`) return { modules };
    if (subject === `${SUB.projector}.modules.get`) return { modules };
    throw new Error(`Unexpected RPC: ${subject}`);
  }
}));

const valkey = await import('@bagel/kit/server/valkey-store');
mock.module('@bagel/kit/server/valkey-store', () => ({ ...valkey, getModules: async () => ({ projected: true, modules }) }));

const { writeLoyalty } = await import('./loyalty-store');
const { setModuleEnabled } = await import('./module-blob');
const { blankLoyaltyConfig } = await import('@bagel/kit');

beforeEach(() => {
  purged.length = 0;
});

test.each([false, true])('saving loyalty enabled=%p purges the channel pages, leaderboard included', async (enabled) => {
  await writeLoyalty('77', enabled, blankLoyaltyConfig());
  expect(purged).toEqual(['77']);
});

test.each([false, true])('toggling loyalty from the module index (enabled=%p) purges the channel pages', async (enabled) => {
  await setModuleEnabled('78', 'loyalty', enabled);
  expect(purged).toEqual(['78']);
});
