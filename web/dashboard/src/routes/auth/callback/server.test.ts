// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, describe, expect, mock, test } from 'bun:test';
import { privateEnv, stubSvelteKit } from '../../../../test/sveltekit';

process.env.SESSION_KEY = Buffer.alloc(32, 7).toString('base64');
stubSvelteKit();
Object.assign(privateEnv, { TWITCH_CLIENT_ID: 'client' });

const { RpcError } = await import('@bagel/kit/server/nats');

let existing: () => Promise<{ username: string }> = async () => ({ username: 'streamer' });
const purged: { kind: 'pages' | 'board'; userId: string; logins: readonly string[] }[] = [];

const services = await import('../../../lib/server/services');
mock.module('#lib/server/services', () => ({
  ...services,
  isBanned: async () => false,
  accountState: () => existing(),
  userLocale: async () => 'en',
  userCursor: async () => true,
  saveGrant: async () => {}
}));
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({ ...nats, rpc: async () => ({ ok: true }) }));
mock.module('#lib/server/edge-purge', () => ({
  schedulePurgeChannel: (userId: string, logins: readonly string[] = []) => void purged.push({ kind: 'pages', userId, logins }),
  purgeRenamedChannel: (userId: string, logins: readonly string[] = []) => {
    purged.push({ kind: 'pages', userId, logins });
    purged.push({ kind: 'board', userId, logins });
  }
}));
mock.module('#lib/server/oauth', () => ({
  safeNextPath: () => null,
  fetchAccountEmail: async () => null,
  twitch: () => ({
    validateAuthorizationCode: async () => ({
      claims: () => ({ sub: '42', preferred_username: 'Streamer', aud: 'client', iss: 'https://id.twitch.tv/oauth2', nonce: 'n' }),
      scopes: () => [],
      accessToken: () => 'a',
      refreshToken: () => 'r'
    })
  })
}));

const { GET } = await import('./+server');

async function signIn(): Promise<number> {
  const jar = new Map([
    ['oauth_state', 's'],
    ['oauth_nonce', 'n']
  ]);
  const cookies = { get: (n: string) => jar.get(n), set: () => {}, delete: (n: string) => void jar.delete(n) };
  try {
    await GET({ url: new URL('https://dashboard.itsbagelbot.com/auth/callback?code=c&state=s'), cookies } as never);
    return 200;
  } catch (e) {
    return (e as { status: number }).status;
  }
}

beforeEach(() => {
  purged.length = 0;
});

describe('auth callback channel purge', () => {
  test('a first sign-in purges the new commands pages and no leaderboard tag', async () => {
    existing = async () => {
      throw new RpcError('ent: user not found', 'not_found');
    };
    expect(await signIn()).toBe(302);
    expect(purged).toEqual([{ kind: 'pages', userId: '42', logins: ['streamer'] }]);
  });

  test('a returning user with the same login purges nothing', async () => {
    existing = async () => ({ username: 'Streamer' });
    expect(await signIn()).toBe(302);
    expect(purged).toEqual([]);
  });

  test('a renamed user purges the commands pages and leaderboard tags of both logins', async () => {
    existing = async () => ({ username: 'oldname' });
    await signIn();
    expect(purged).toEqual([
      { kind: 'pages', userId: '42', logins: ['streamer', 'oldname'] },
      { kind: 'board', userId: '42', logins: ['streamer', 'oldname'] }
    ]);
  });

  test('a rename drops the stale login lookup and account snapshot from L1', async () => {
    existing = async () => ({ username: 'oldname' });
    const { fabric } = services;
    fabric.cache.set('login:oldname', 'stale', 60_000);
    fabric.cache.set('state:42', 'stale', 60_000);
    await signIn();
    const reload = (key: string) => fabric.readKey(key, 60_000, async () => 'fresh');
    expect([await reload('login:oldname'), await reload('state:42')]).toEqual(['fresh', 'fresh']);
  });

  test('an unreadable account state purges nothing and relies on the short 404 TTL', async () => {
    existing = async () => {
      throw new Error('timeout');
    };
    await signIn();
    expect(purged).toEqual([]);
  });
});
