// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';

let commandsPageReply: (id: string) => Promise<boolean> = async () => true;
let listCommandsCalls = 0;
let listModulesCalls = 0;
let loyaltyFails = false;

mock.module('$app/environment', () => ({ dev: true }));
mock.module('$lib/server/loyalty-store', () => ({
  readLoyalty: async () => {
    if (loyaltyFails) throw new Error('loyalty down');
    return { config: { pointsName: 'bagels' } };
  },
  topStandings: async () => [{ viewerId: '1', viewerLogin: 'fan', viewerName: 'Fan', points: 10, watchSeconds: 60 }]
}));
mock.module('$lib/server/commands-store', () => ({
  listCommands: async () => {
    listCommandsCalls++;
    return [{ trigger: '!hello' }];
  },
  listModules: async () => {
    listModulesCalls++;
    return [{ id: 'feed' }];
  }
}));
mock.module('$lib/server/public-directory', () => ({
  channelLabel: (record: { displayName?: string; username?: string } | null | undefined, fallback: string) =>
    record?.displayName || record?.username || fallback,
  publicCommands: () => [{ trigger: '!hello', aliases: [], response: 'hi', perm: 'everyone' }],
  publicModules: () => [{ id: 'feed', commands: [{ label: '!feed' }] }]
}));
mock.module('$lib/server/services', () => ({
  resolveLogin: async (login: string) => ({ userId: '42', username: login }),
  userCommandsPage: (id: string) => commandsPageReply(id)
}));

const { load } = await import('./+page.server');

type LoadArgs = Parameters<typeof load>[0];
type LoadResult = { top: unknown[]; degraded: boolean; commands: unknown[]; modules: unknown[] };

async function runLoad(): Promise<LoadResult> {
  const event = {
    params: { user: 'somechannel' },
    url: new URL('https://leaderboard.itsbagelbot.com/somechannel'),
    locals: { locale: 'en' }
  } as unknown as LoadArgs;
  return (await load(event)) as unknown as LoadResult;
}

describe('(public)/[user] load: commands-page toggle', () => {
  beforeEach(() => {
    commandsPageReply = async () => true;
    listCommandsCalls = 0;
    listModulesCalls = 0;
    loyaltyFails = false;
  });

  test('a visible commands page renders commands and modules', async () => {
    const out = await runLoad();
    expect([out.commands.length, out.modules.length, listCommandsCalls, listModulesCalls]).toEqual([1, 1, 1, 1]);
  });

  test('a hidden commands page keeps standings, skips the directory RPCs and returns empty lists', async () => {
    commandsPageReply = async () => false;

    const out = await runLoad();
    expect([out.top.length, out.degraded, out.commands, out.modules]).toEqual([1, false, [], []]);
    expect([listCommandsCalls, listModulesCalls]).toEqual([0, 0]);
  });

  test('a rejected commands-page read fails open like the commands page', async () => {
    commandsPageReply = async () => {
      throw new Error('users service blip');
    };

    const out = await runLoad();
    expect([out.commands.length, out.modules.length]).toEqual([1, 1]);
  });

  test('a hidden commands page does not hide a degraded standings notice', async () => {
    commandsPageReply = async () => false;
    loyaltyFails = true;

    const out = await runLoad();
    expect([out.degraded, out.top, out.commands]).toEqual([true, [], []]);
  });
});
