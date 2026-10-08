// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit({ dev: true });

type Page = { top: unknown[]; degraded: boolean; commands: unknown[]; modules: unknown[] };

const directoryReads: string[] = [];
let commandsPageReply: (id: string) => Promise<boolean> = async () => true;
let loyaltyFails = false;

mock.module('$lib/server/loyalty-store', () => ({
  readLoyalty: async () => {
    if (loyaltyFails) throw new Error('loyalty down');
    return { config: { pointsName: 'bagels' } };
  },
  topStandings: async () => [{ viewerId: '1', viewerLogin: 'fan', viewerName: 'Fan', points: 10, watchSeconds: 60 }]
}));
mock.module('$lib/server/commands-store', () => ({
  listCommands: async () => {
    directoryReads.push('commands');
    return [{ name: 'hello', response: 'hi', is_active: true }];
  },
  listModules: async () => {
    directoryReads.push('modules');
    return [];
  }
}));
mock.module('$lib/server/services', () => ({
  resolveLogin: async (login: string) => ({ userId: '42', username: login }),
  userCommandsPage: (id: string) => commandsPageReply(id)
}));

const { load } = await import('./+page.server');

async function loadPage(): Promise<Page> {
  const event = {
    params: { user: 'somechannel' },
    url: new URL('https://leaderboard.itsbagelbot.com/somechannel'),
    locals: { locale: 'en' }
  };
  return (await load(event as never)) as unknown as Page;
}

const cases: {
  name: string;
  commandsPage: () => Promise<boolean>;
  loyaltyFails?: boolean;
  want: { commands: string[]; modulesListed: boolean; top: number; degraded: boolean; reads: string[] };
}[] = [
  {
    name: 'a visible commands page renders commands and modules',
    commandsPage: async () => true,
    want: { commands: ['!hello'], modulesListed: true, top: 1, degraded: false, reads: ['commands', 'modules'] }
  },
  {
    name: 'a hidden commands page keeps standings, skips the directory RPCs and returns empty lists',
    commandsPage: async () => false,
    want: { commands: [], modulesListed: false, top: 1, degraded: false, reads: [] }
  },
  {
    name: 'a rejected commands-page read fails open like the commands page',
    commandsPage: async () => {
      throw new Error('users service blip');
    },
    want: { commands: ['!hello'], modulesListed: true, top: 1, degraded: false, reads: ['commands', 'modules'] }
  },
  {
    name: 'a hidden commands page does not hide a degraded standings notice',
    commandsPage: async () => false,
    loyaltyFails: true,
    want: { commands: [], modulesListed: false, top: 0, degraded: true, reads: [] }
  }
];

describe('(public)/[user] load: commands-page toggle', () => {
  test.each(cases)('$name', async ({ commandsPage, loyaltyFails: fails, want }) => {
    directoryReads.length = 0;
    commandsPageReply = commandsPage;
    loyaltyFails = fails ?? false;
    const page = await loadPage();
    expect({
      commands: page.commands.map((c) => (c as { trigger: string }).trigger),
      modulesListed: page.modules.length > 0,
      top: page.top.length,
      degraded: page.degraded,
      reads: directoryReads
    }).toEqual(want);
  });
});
