// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { describe, expect, mock, test } from 'bun:test';

let resolveLoginReply: (login: string) => Promise<{ userId: string; username?: string } | null> = async () => null;
let commandsPageReply: (id: string) => Promise<boolean> = async () => true;

mock.module('$app/environment', () => ({ dev: true }));
mock.module('$env/dynamic/private', () => ({ env: {} }));
mock.module('$lib/server/seo-hosts', () => ({ requireHost: () => {} }));
mock.module('$lib/server/commands-store', () => ({
  listCommands: async () => [],
  listModules: async () => []
}));
mock.module('$lib/server/public-directory', () => ({
  channelLabel: (record: { displayName?: string; username?: string } | null | undefined, fallback: string) =>
    record?.displayName || record?.username || fallback,
  publicCommands: () => [],
  publicModules: () => []
}));
mock.module('$lib/server/services', () => ({
  accountState: async (userId: string) => ({ active: true, status: 'free', onboarded: true, creatorCode: null, username: '', displayName: '', userId }),
  resolveLogin: (login: string) => resolveLoginReply(login),
  userCommandsPage: (id: string) => commandsPageReply(id)
}));

const { load } = await import('./+page.server');

type LoadArgs = Parameters<typeof load>[0];

function makeEvent(channel: string): { event: LoadArgs; locals: Record<string, unknown> } {
  const locals: Record<string, unknown> = {};
  const event = {
    params: { channel },
    url: new URL(`https://commands.itsbagelbot.com/user/${channel}`),
    locals
  } as unknown as LoadArgs;
  return { event, locals };
}

async function runLoad(channel: string): Promise<{ result?: unknown; status?: number; message?: string; locals: Record<string, unknown> }> {
  const { event, locals } = makeEvent(channel);
  try {
    const result = await load(event);
    return { result, locals };
  } catch (e) {
    const err = e as { status?: number; body?: { message?: string } };
    return { status: err.status, message: err.body?.message, locals };
  }
}

describe('(public)/user/[channel] load: commands-page toggle', () => {
  test('a hidden channel 404s with the unknown-channel message and sets locals.edgeCache404', async () => {
    resolveLoginReply = async (login) => ({ userId: '42', username: login });
    commandsPageReply = async () => false;

    const out = await runLoad('somechannel');
    expect([out.status, out.message, out.locals.edgeCache404]).toEqual([404, 'Channel not found', true]);
  });

  test('an unknown login 404s without the flag', async () => {
    resolveLoginReply = async () => null;
    commandsPageReply = async () => true;

    const out = await runLoad('nosuchchannel');
    expect([out.status, out.message, 'edgeCache404' in out.locals]).toEqual([404, 'Channel not found', false]);
  });

  test('a rejected commands-page read fails open and renders', async () => {
    resolveLoginReply = async (login) => ({ userId: '42', username: login });
    commandsPageReply = async () => {
      throw new Error('users service blip');
    };

    const out = await runLoad('somechannel');
    expect(out.status).toBeUndefined();
    expect(out.locals.edgeCache404).toBeUndefined();
    expect((out.result as { degraded: boolean }).degraded).toBe(false);
  });
});
