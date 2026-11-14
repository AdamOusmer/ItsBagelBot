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

let banned = false;
const events: string[] = [];

async function step<T>(name: string, value: T): Promise<T> {
  events.push(`start:${name}`);
  await new Promise((resolve) => setTimeout(resolve, 0));
  events.push(`end:${name}`);
  return value;
}

const services = await import('../../../lib/server/services');
mock.module('#lib/server/services', () => ({
  ...services,
  isBanned: () => step('banned', banned),
  accountState: async () => {
    events.push('start:prior');
    const state = await existing();
    events.push('end:prior');
    return state;
  },
  userLocale: () => step('locale', 'fr'),
  userCursor: () => step('cursor', true),
  saveGrant: () => step('grant', undefined),
  delegationConsume: () => step('delegation', { ok: true, owner_user_id: '7', owner_login: 'owner', sections: [] })
}));
const nats = await import('@bagel/kit/server/nats');
mock.module('@bagel/kit/server/nats', () => ({
  ...nats,
  rpc: async () => {
    events.push('register');
    return { ok: true };
  }
}));
mock.module('#lib/server/edge-purge', () => ({
  schedulePurgeChannel: (userId: string, logins: readonly string[] = []) => void purged.push({ kind: 'pages', userId, logins }),
  purgeRenamedChannel: (userId: string, logins: readonly string[] = []) => {
    purged.push({ kind: 'pages', userId, logins });
    purged.push({ kind: 'board', userId, logins });
  }
}));
mock.module('#lib/server/oauth', () => ({
  safeNextPath: () => null,
  fetchAccountEmail: () => step('email', null),
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

let written = new Map<string, string>();

async function signInTo(extra: [string, string][] = []): Promise<{ status: number; location: string | null }> {
  const jar = new Map([['oauth_state', 's'], ['oauth_nonce', 'n'], ...extra]);
  written = new Map();
  const cookies = {
    get: (n: string) => jar.get(n),
    set: (n: string, value: string) => void written.set(n, value),
    delete: (n: string) => void jar.delete(n)
  };
  try {
    await GET({ url: new URL('https://dashboard.itsbagelbot.com/auth/callback?code=c&state=s'), cookies } as never);
    return { status: 200, location: null };
  } catch (e) {
    const { status, location } = e as { status: number; location: string };
    return { status, location };
  }
}

async function signIn(): Promise<number> {
  return (await signInTo()).status;
}

beforeEach(() => {
  purged.length = 0;
  events.length = 0;
  banned = false;
  existing = async () => ({ username: 'streamer' });
});

describe('auth callback ordering', () => {
  test('a banned user is sent to the banned page with no write and no cookie', async () => {
    banned = true;
    expect(await signInTo()).toEqual({ status: 302, location: '/login?e=banned' });
    expect(events.filter((e) => e === 'register' || e.endsWith(':grant'))).toEqual([]);
    expect([...written.keys()]).toEqual([]);
  });

  test('the ban, email and prior-login reads overlap and all finish before the user is registered', async () => {
    await signInTo();
    expect(events.slice(0, 3)).toEqual(['start:banned', 'start:email', 'start:prior']);
    expect(events.slice(3, 6).sort()).toEqual(['end:banned', 'end:email', 'end:prior']);
    expect(events[6]).toBe('register');
  });

  test('locale, cursor and grant run together after registration and set the same cookies', async () => {
    const result = await signInTo();
    expect(events.slice(7, 10).sort()).toEqual(['start:cursor', 'start:grant', 'start:locale']);
    expect(result).toEqual({ status: 302, location: '/' });
    expect([...written.keys()].sort()).toEqual(['bagel_cursor', 'bagel_session', 'locale']);
    expect([written.get('locale'), written.get('bagel_cursor')]).toEqual(['fr', '1']);
  });

  test('an invitee accepting a team link still stores their own Twitch grant', async () => {
    const result = await signInTo([['pending_delegation', 'invite']]);
    expect(result).toEqual({ status: 302, location: '/' });
    expect(events.filter((e) => e === 'end:grant' || e === 'start:delegation')).toEqual(['end:grant', 'start:delegation']);
  });
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
