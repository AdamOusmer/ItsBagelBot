// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../test/sveltekit';

stubSvelteKit();

const pagePurges: string[] = [];
const boardPurges: string[] = [];
mock.module('./edge-purge', () => ({
  schedulePurgeChannel: (userId: string) => void pagePurges.push(userId),
  scheduleLeaderboardPurge: (userId: string) => void boardPurges.push(userId)
}));

let storedLoyalty = true;
const stored = () => [{ name: 'loyalty', is_enabled: storedLoyalty, revision: 4, configs: {} }];
const nats = await import('@bagel/kit/server/nats');
const { SUB } = await import('./services');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async (subject: string) => {
    if (subject === `${SUB.modules}.upsert`) return { modules: stored() };
    if (subject === `${SUB.projector}.modules.replace`) return { modules: stored() };
    if (subject === `${SUB.projector}.modules.get`) return { modules: stored() };
    throw new Error(`Unexpected RPC: ${subject}`);
  }
}));

const valkey = await import('@bagel/kit/server/valkey-store');
mock.module('@bagel/kit/server/valkey-store', () => ({ ...valkey, getModules: async () => ({ projected: true, modules: stored() }) }));

const { writeLoyalty } = await import('./loyalty-store');
const { setModuleEnabled } = await import('./module-blob');
const { upsertModule } = await import('./commands-store');
const { blankLoyaltyConfig } = await import('@bagel/kit');

let nextUser = 100;
const freshUser = () => String(nextUser++);

beforeEach(() => {
  pagePurges.length = 0;
  boardPurges.length = 0;
});

describe('loyalty writes and the leaderboard tag purge', () => {
  test.each([
    { before: false, after: true },
    { before: true, after: false }
  ])('the loyalty page turning loyalty $before -> $after purges the commands URLs and the leaderboard tag', async ({ before, after }) => {
    const user = freshUser();
    storedLoyalty = before;
    await writeLoyalty(user, after, blankLoyaltyConfig());
    expect({ pages: pagePurges, boards: boardPurges }).toEqual({ pages: [user], boards: [user] });
  });

  test('the module index toggle purges the leaderboard tag when loyalty flips', async () => {
    const user = freshUser();
    storedLoyalty = true;
    await setModuleEnabled(user, 'loyalty', false);
    expect(boardPurges).toEqual([user]);
  });

  test('a loyalty settings save that keeps it on never tag-purges', async () => {
    const user = freshUser();
    storedLoyalty = true;
    await writeLoyalty(user, true, blankLoyaltyConfig());
    expect({ pages: pagePurges, boards: boardPurges }).toEqual({ pages: [user], boards: [] });
  });

  test('other module writes never tag-purge', async () => {
    const user = freshUser();
    await upsertModule(user, 'timers', false, {});
    expect({ pages: pagePurges, boards: boardPurges }).toEqual({ pages: [user], boards: [] });
  });
});
