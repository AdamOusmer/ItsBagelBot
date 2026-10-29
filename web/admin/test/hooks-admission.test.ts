// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { describe, expect, mock, test } from 'bun:test';
import { stubSvelteKit } from './sveltekit';

process.env.SESSION_KEY = Buffer.alloc(32, 7).toString('base64');
stubSvelteKit();

let adminReply: { id: string } | null = null;
mock.module('#lib/server/access', () => ({ requireAdmin: async () => adminReply }));
mock.module('#lib/server/lanes', () => ({ ensureLaneStoreHA: async () => {} }));
mock.module('#lib/server/services', () => ({ startInvalidationListener() {} }));
mock.module('@bagel/kit/server/boot', () => ({ initConsoleRuntime() {} }));
mock.module('newrelic', () => ({ default: { noticeError() {}, addCustomAttributes() {}, setTransactionName() {} } }));
mock.module('@bagel/kit/server/analytics', () => ({ countView() {}, setAnalyticsApp() {} }));
mock.module('virtual:bagel-i18n', () => ({ urls: {}, loaders: {}, sliced: false }));
mock.module('@bagel/kit/i18n', () => ({
  DEFAULT_LOCALE: 'en',
  LOCALES: ['en'],
  LOCALE_COOKIE: 'locale',
  isLocale: (value: unknown) => value === 'en',
  detectLocale: () => 'en',
  ensureCatalog: async () => {},
  translate: (_locale: string, key: string) => key
}));

const { handle } = await import('../src/hooks.server');
const { COOKIE, seal } = await import('../src/lib/server/session');

function event(path: string, sessionCookie?: string) {
  const url = new URL(`https://admin.example${path}`);
  return {
    url,
    request: new Request(url),
    route: { id: null },
    isDataRequest: path.includes('__data.json'),
    locals: {},
    cookies: {
      get: (name: string) => (name === COOKIE ? sessionCookie : undefined),
      getAll: () => [],
      set() {},
      delete() {}
    },
    getClientAddress: () => '127.0.0.1'
  };
}

async function admit(path: string, sessionCookie?: string): Promise<{ status: number; location?: string; resolved: boolean }> {
  let resolved = false;
  try {
    const res = await handle({
      event: event(path, sessionCookie) as never,
      resolve: async () => {
        resolved = true;
        return new Response('ok');
      }
    });
    return { status: res.status, resolved };
  } catch (thrown) {
    const r = thrown as { status: number; location?: string };
    return { status: r.status, location: r.location, resolved };
  }
}

describe('staff admission', () => {
  test.each([
    '/notifications/__data.json?x-sveltekit-invalidated=001',
    '/lanes/__data.json?x-sveltekit-invalidated=001',
    '/notifications',
    '/'
  ])('a cookieless request for %s goes to sign-in without resolving', async (path) => {
    expect(await admit(path)).toEqual({ status: 303, location: '/login', resolved: false });
  });

  test.each(['/login', '/auth/login', '/healthz', '/auth/bot/callback'])('public route %s resolves without a session', async (path) => {
    expect((await admit(path)).resolved).toBe(true);
  });

  test('a signed-in staff member resolves', async () => {
    adminReply = { id: '1' };
    const now = Math.floor(Date.now() / 1000);
    const cookie = seal({ user_id: '1', login: 'mod', display_name: 'Mod', role: 'mod', iat: now, expires_at: now + 3600 });
    expect((await admit('/notifications/__data.json', cookie)).resolved).toBe(true);
    adminReply = null;
  });
});
