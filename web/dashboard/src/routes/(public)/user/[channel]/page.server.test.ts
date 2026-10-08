// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../../test/sveltekit';

stubSvelteKit({ dev: true });

type Resolved = { userId: string; username?: string } | null;

let resolveLoginReply: (login: string) => Promise<Resolved> = async () => null;
let commandsPageReply: (id: string) => Promise<boolean> = async () => true;

mock.module('$lib/server/commands-store', () => ({ listCommands: async () => [], listModules: async () => [] }));
mock.module('$lib/server/services', () => ({
  accountState: async () => ({ creatorCode: null, username: '', displayName: '' }),
  resolveLogin: (login: string) => resolveLoginReply(login),
  userCommandsPage: (id: string) => commandsPageReply(id)
}));

const { load } = await import('./+page.server');

const found = async (login: string): Promise<Resolved> => ({ userId: '42', username: login });

async function outcome(channel: string) {
  const locals: Record<string, unknown> = {};
  const event = { params: { channel }, url: new URL(`https://commands.itsbagelbot.com/user/${channel}`), locals };
  try {
    const result = (await load(event as never)) as { degraded: boolean };
    return { degraded: result.degraded, edgeCache404: locals.edgeCache404 };
  } catch (e) {
    const err = e as { status?: number; body?: { message?: string } };
    return { status: err.status, message: err.body?.message, edgeCache404: locals.edgeCache404 };
  }
}

const cases: { name: string; resolve: typeof resolveLoginReply; page: typeof commandsPageReply; channel: string; want: Awaited<ReturnType<typeof outcome>> }[] = [
  {
    name: 'a hidden channel 404s with the unknown-channel message and sets locals.edgeCache404',
    resolve: found,
    page: async () => false,
    channel: 'somechannel',
    want: { status: 404, message: 'Channel not found', edgeCache404: true }
  },
  {
    name: 'an unknown login 404s without the flag',
    resolve: async () => null,
    page: async () => true,
    channel: 'nosuchchannel',
    want: { status: 404, message: 'Channel not found', edgeCache404: undefined }
  },
  {
    name: 'a rejected commands-page read fails open and renders',
    resolve: found,
    page: async () => {
      throw new Error('users service blip');
    },
    channel: 'somechannel',
    want: { degraded: false, edgeCache404: undefined }
  }
];

describe('(public)/user/[channel] load: commands-page toggle', () => {
  test.each(cases)('$name', async ({ resolve, page, channel, want }) => {
    resolveLoginReply = resolve;
    commandsPageReply = page;
    expect(await outcome(channel)).toEqual(want);
  });
});
