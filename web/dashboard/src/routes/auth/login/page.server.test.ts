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
  twitch: () => ({ createAuthorizationURL: () => new URL('https://id.twitch.tv/oauth2/authorize') })
}));
const { load } = await import('./+page.server');
beforeEach(() => { counts.length = 0; });

test.each([false, true])('counts only an actual authorization start with session=%s', async (signedIn) => {
  const cookies = { get: () => undefined, set: () => {}, delete: () => {} };
  try {
    await load({ cookies, url: new URL('https://dashboard.example/auth/login?e=banned'), locals: { session: signedIn ? {} : null } } as never);
  } catch (err) {
    expect((err as { status: number }).status).toBe(302);
  }
  expect(counts).toEqual(signedIn ? [] : [['auth.login', 'started']]);
});
