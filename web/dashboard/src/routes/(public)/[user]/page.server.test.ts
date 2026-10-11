// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit({ dev: true });

type Page = { top: unknown[]; degraded: boolean; commandsPage: boolean; commands: unknown[]; modules: unknown[] };

const directoryReads: string[] = [];
let commandsPageReply: (id: string) => Promise<boolean> = async () => true;
let loyaltyFails = false;
let loyaltyEnabled = true;
let leaderboardPageReply: (id: string) => Promise<boolean> = async () => true;
let standingsReads = 0;

mock.module('$lib/server/loyalty-store', () => ({
  readLoyalty: async () => {
    if (loyaltyFails) throw new Error('loyalty down');
    return { enabled: loyaltyEnabled, config: { pointsName: 'bagels' } };
  },
  topStandings: async () => {
    standingsReads++;
    return [{ viewerId: '1', viewerLogin: 'fan', viewerName: 'Fan', points: 10, watchSeconds: 60 }];
  }
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
  userCommandsPage: (id: string) => commandsPageReply(id),
  userLeaderboardPage: (id: string) => leaderboardPageReply(id)
}));

const { load } = await import('./+page.server');

type Outcome = { status?: number; message?: string; edgeCache404?: boolean; page?: Page };

async function loadPage(): Promise<Outcome> {
  const locals: Record<string, unknown> = { locale: 'en' };
  const event = {
    params: { user: 'somechannel' },
    url: new URL('https://leaderboard.itsbagelbot.com/somechannel'),
    locals
  };
  try {
    return { page: (await load(event as never)) as unknown as Page, edgeCache404: locals.edgeCache404 as boolean | undefined };
  } catch (e) {
    const err = e as { status?: number; body?: { message?: string } };
    return { status: err.status, message: err.body?.message, edgeCache404: locals.edgeCache404 as boolean | undefined };
  }
}

type Setup = {
  commandsPage?: () => Promise<boolean>;
  leaderboardPage?: () => Promise<boolean>;
  loyaltyFails?: boolean;
  loyaltyEnabled?: boolean;
};

function arrange(setup: Setup): void {
  directoryReads.length = 0;
  standingsReads = 0;
  commandsPageReply = setup.commandsPage ?? (async () => true);
  leaderboardPageReply = setup.leaderboardPage ?? (async () => true);
  loyaltyFails = setup.loyaltyFails ?? false;
  loyaltyEnabled = setup.loyaltyEnabled ?? true;
}

const cases: {
  name: string;
  commandsPage: () => Promise<boolean>;
  loyaltyFails?: boolean;
  want: { commands: string[]; modulesListed: boolean; top: number; degraded: boolean; commandsPage: boolean; reads: string[] };
}[] = [
  {
    name: 'a visible commands page renders commands and modules',
    commandsPage: async () => true,
    want: { commands: ['!hello'], modulesListed: true, top: 1, degraded: false, commandsPage: true, reads: ['commands', 'modules'] }
  },
  {
    name: 'a hidden commands page keeps standings, skips the directory RPCs and returns empty lists',
    commandsPage: async () => false,
    want: { commands: [], modulesListed: false, top: 1, degraded: false, commandsPage: false, reads: [] }
  },
  {
    name: 'a rejected commands-page read fails open like the commands page',
    commandsPage: async () => {
      throw new Error('users service blip');
    },
    want: { commands: ['!hello'], modulesListed: true, top: 1, degraded: false, commandsPage: true, reads: ['commands', 'modules'] }
  },
  {
    name: 'a hidden commands page does not hide a degraded standings notice',
    commandsPage: async () => false,
    loyaltyFails: true,
    want: { commands: [], modulesListed: false, top: 0, degraded: true, commandsPage: false, reads: [] }
  }
];

describe('(public)/[user] load: commands-page toggle', () => {
  test.each(cases)('$name', async ({ commandsPage, loyaltyFails: fails, want }) => {
    arrange({ commandsPage, loyaltyFails: fails });
    const { page } = await loadPage();
    expect({
      commands: page!.commands.map((c) => (c as { trigger: string }).trigger),
      modulesListed: page!.modules.length > 0,
      top: page!.top.length,
      degraded: page!.degraded,
      commandsPage: page!.commandsPage,
      reads: directoryReads
    }).toEqual(want);
  });
});

const notFound = { status: 404, message: 'Channel not found', edgeCache404: true };

describe('(public)/[user] load: leaderboard visibility', () => {
  test('a hidden leaderboard 404s edge-cached with the unknown-channel message and reads nothing else', async () => {
    arrange({ leaderboardPage: async () => false });
    const out = await loadPage();
    expect({ ...out }).toEqual(notFound);
    expect({ standingsReads, directoryReads }).toEqual({ standingsReads: 0, directoryReads: [] });
  });

  test('a disabled loyalty module 404s edge-cached', async () => {
    arrange({ loyaltyEnabled: false });
    expect(await loadPage()).toEqual(notFound);
    expect(standingsReads).toBe(0);
  });

  test('a rejected leaderboard-flag read fails open and renders', async () => {
    arrange({
      leaderboardPage: async () => {
        throw new Error('users service blip');
      }
    });
    const out = await loadPage();
    expect({ status: out.status, edgeCache404: out.edgeCache404, top: out.page?.top.length }).toEqual({
      status: undefined,
      edgeCache404: undefined,
      top: 1
    });
  });

  test('a rejected loyalty read renders the degraded page instead of a 404', async () => {
    arrange({ loyaltyFails: true });
    const out = await loadPage();
    expect({ status: out.status, edgeCache404: out.edgeCache404, degraded: out.page?.degraded }).toEqual({
      status: undefined,
      edgeCache404: undefined,
      degraded: true
    });
  });
});
