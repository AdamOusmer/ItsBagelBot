// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { beforeEach, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();
const counts: string[][] = [];
mock.module('@bagel/kit/server/analytics', () => ({ count: (...args: string[]) => counts.push(args) }));
mock.module('#lib/server/oauth', () => ({
  safeNextPath: () => null,
  scopes: () => ['openid'],
  twitch: () => ({ createAuthorizationURL: (state: string) => new URL(`https://id.twitch.tv/oauth2/authorize?state=${state}`) })
}));
mock.module('@bagel/kit/server/oauth', () => ({ generateState: () => 's' }));
const { load } = await import('./+page.server');
beforeEach(() => { counts.length = 0; });

test.each([false, true])('counts only an actual authorization start with session=%s', async (signedIn) => {
  const cookies = { get: () => undefined, set: () => {}, delete: () => {} };
  try {
    await load({ cookies, url: new URL('https://dashboard.example/auth/login?e=banned'), locals: { session: signedIn ? {} : null } } as never);
  } catch (err) {
    expect((err as { status: number }).status).toBe(302);
  }
  expect(counts).toEqual(signedIn ? [] : [['auth.login', 'started'], ['signup.via', 'none', 'started']]);
});

test.each([
  ['?via=chatgpt', 'chatgpt', 's.chatgpt'],
  ['?via=private-text', 'none', 's.none'],
  ['', 'none', 's.none']
])('carries the source %p in the OAuth state only', async (query, via, sent) => {
  const stored = new Map<string, string>();
  const cookies = { get: () => undefined, set: (n: string, v: string) => void stored.set(n, v), delete: () => {} };
  let location = '';
  try {
    await load({ cookies, url: new URL(`https://dashboard.example/auth/login${query}`), locals: { session: null } } as never);
  } catch (err) {
    location = (err as { location: string }).location;
  }
  expect([counts.at(-1), stored.get('oauth_state'), new URL(location).searchParams.get('state')]).toEqual([['signup.via', via, 'started'], 's', sent]);
});
