// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../../test/sveltekit';

stubSvelteKit({ dev: true });

type Resolved = { userId: string; username?: string } | null;

const { RpcError } = await import('@bagel/kit/server/nats');

let resolveLoginReply: (login: string) => Promise<Resolved> = async () => null;
let commandsPageReply: (id: string) => Promise<boolean> = async () => true;

let listFails = false;
let healReply: { kind: string; value?: Resolved } = { kind: 'missing' };

mock.module('#lib/server/commands-store', () => ({
  listCommands: async () => {
    if (listFails) throw new Error('commands down');
    return [{ name: 'hello', response: 'hi', is_active: true }];
  },
  listModules: async () => []
}));
mock.module('#lib/server/services', () => ({
  accountState: async () => ({ creatorCode: null, username: '', displayName: '' }),
  resolveLogin: (login: string) => resolveLoginReply(login),
  userCommandsPage: (id: string) => commandsPageReply(id)
}));

mock.module('#lib/server/identity-heal', () => ({ healLogin: async () => healReply }));

const { load } = await import('./+page.server');

const found = async (login: string): Promise<Resolved> => ({ userId: '42', username: login });

type Outcome = { degraded?: boolean; commands?: number; status?: number; message?: string; location?: string; edge?: unknown };

async function outcome(channel: string): Promise<Outcome> {
  const locals: Record<string, unknown> = {};
  const event = { params: { channel }, url: new URL(`https://commands.itsbagelbot.com/user/${channel}`), locals };
  try {
    const result = (await load(event as never)) as { degraded: boolean; commands: unknown[] };
    return { degraded: result.degraded, commands: result.commands.length, edge: locals.edgeOutcome };
  } catch (e) {
    const err = e as { status?: number; body?: { message?: string }; location?: string };
    return { status: err.status, message: err.body?.message, location: err.location, edge: locals.edgeOutcome };
  }
}

const notFound = { status: 404, message: 'Channel not found' };

const cases: { name: string; resolve: typeof resolveLoginReply; page: typeof commandsPageReply; channel: string; want: Outcome }[] = [
  {
    name: 'a hidden channel 404s with the unknown-channel message and is edge-cacheable',
    resolve: found,
    page: async () => false,
    channel: 'somechannel',
    want: { ...notFound, edge: 'hidden' }
  },
  {
    name: 'an empty login reply 404s as a definitive miss',
    resolve: async () => null,
    page: async () => true,
    channel: 'nosuchchannel',
    want: { ...notFound, edge: 'missing' }
  },
  {
    name: 'a not_found refusal 404s as a definitive miss',
    resolve: async () => {
      throw new RpcError('ent: user not found', 'not_found');
    },
    page: async () => true,
    channel: 'nosuchchannel',
    want: { ...notFound, edge: 'missing' }
  },
  {
    name: 'an unreachable users service 404s without an edge outcome',
    resolve: async () => {
      throw new Error('timeout');
    },
    page: async () => true,
    channel: 'somechannel',
    want: { ...notFound, edge: undefined }
  },
  {
    name: 'a visible channel renders as a cacheable page',
    resolve: found,
    page: async () => true,
    channel: 'somechannel',
    want: { degraded: false, commands: 1, edge: 'page' }
  },
  {
    name: 'a rejected commands-page read fails open but is never edge-cached',
    resolve: found,
    page: async () => {
      throw new Error('users service blip');
    },
    channel: 'somechannel',
    want: { degraded: false, commands: 1, edge: 'skip' }
  }
];

test('a degraded directory renders but is never edge-cached', async () => {
  resolveLoginReply = found;
  commandsPageReply = async () => true;
  listFails = true;
  try {
    expect(await outcome('somechannel')).toEqual({ degraded: true, commands: 0, edge: 'skip' });
  } finally {
    listFails = false;
  }
});

describe('(public)/user/[channel] load: commands-page toggle', () => {
  test.each(cases)('$name', async ({ resolve, page, channel, want }) => {
    resolveLoginReply = resolve;
    commandsPageReply = page;
    expect(await outcome(channel)).toEqual(want);
  });
});

describe('(public)/user/[channel] load: one URL per spelling', () => {
  const spellings: { raw: string; want: Outcome }[] = [
    { raw: 'somechannel', want: { degraded: false, commands: 1, edge: 'page' } },
    { raw: '@somechannel', want: { degraded: false, commands: 1, edge: 'page' } },
    { raw: 'SomeChannel', want: { status: 308, location: '/user/somechannel', edge: undefined } },
    { raw: '@@somechannel', want: { status: 308, location: '/user/somechannel', edge: undefined } }
  ];

  test.each(spellings)('$raw', async ({ raw, want }) => {
    resolveLoginReply = found;
    commandsPageReply = async () => true;
    expect(await outcome(raw)).toEqual(want);
  });
});

describe('(public)/user/[channel] load: renamed channel self-heal', () => {
  const miss = async (): Promise<Resolved> => null;

  test('a login the users DB missed but Twitch and identity_sync resolve renders now', async () => {
    resolveLoginReply = miss;
    commandsPageReply = async () => true;
    healReply = { kind: 'found', value: { userId: '42', username: 'newname' } };
    expect(await outcome('newname')).toEqual({ degraded: false, commands: 1, edge: 'page' });
  });

  test.each([
    { kind: 'missing', edge: 'missing' },
    { kind: 'busy', edge: undefined },
    { kind: 'error', edge: undefined }
  ])('a $kind heal 404s with edge outcome $edge', async ({ kind, edge }) => {
    resolveLoginReply = miss;
    healReply = { kind };
    expect(await outcome('newname')).toEqual({ ...notFound, edge });
  });
});
