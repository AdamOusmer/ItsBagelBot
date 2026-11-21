// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// @ts-ignore Bun supplies this module at test runtime; it is not a production dependency.
import { afterEach, beforeEach, describe, expect, mock, spyOn, test, type Mock } from 'bun:test';
import type { AdminIdentity } from '../src/lib/server/access';
import type { AdminCheck } from '../src/lib/server/services';
import type { Session } from '../src/lib/server/session';
import { stubSvelteKit } from './sveltekit';

process.env.SESSION_KEY = Buffer.alloc(32, 7).toString('base64');
stubSvelteKit();

mock.module('#lib/server/lanes', () => ({ ensureLaneStoreHA: async () => {} }));
mock.module('@bagel/kit/server/boot', () => ({ initConsoleRuntime() {} }));
mock.module('newrelic', () => ({ default: { noticeError() {}, addCustomAttributes() {}, setTransactionName() {}, getTransaction: () => ({ ignore() {} }) } }));
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
const { requireAdmin } = await import('../src/lib/server/access');
const { COOKIE, seal } = await import('../src/lib/server/session');
const services = await import('../src/lib/server/services');
let adminCheck: Mock<typeof services.adminCheck>;

beforeEach(() => {
  adminCheck = spyOn(services, 'adminCheck').mockResolvedValue({ admin: false });
});

afterEach(() => {
  adminCheck.mockRestore();
});

function staffSession(): Session {
  const now = Math.floor(Date.now() / 1000);
  return { user_id: '1', login: 'mod', display_name: 'Mod', role: 'mod', iat: now, expires_at: now + 3600 };
}

function event(path: string, sessionCookie?: string) {
  const url = new URL(`https://admin.example${path}`);
  return {
    url,
    request: new Request(url),
    route: { id: null },
    isDataRequest: path.includes('__data.json'),
    locals: { session: null as Session | null },
    cookies: {
      get: (name: string) => (name === COOKIE ? sessionCookie : undefined),
      getAll: () => [],
      set() {},
      delete() {}
    },
    getClientAddress: () => '127.0.0.1'
  };
}

async function admit(
  path: string,
  sessionCookie?: string,
  onResolve?: (session: Session | null) => Promise<unknown>
): Promise<{ status: number; location?: string; resolved: boolean }> {
  const request = event(path, sessionCookie);
  let resolved = false;
  try {
    const res = await handle({
      event: request as never,
      resolve: async () => {
        resolved = true;
        await onResolve?.(request.locals.session);
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

  test('signed-in staff share one lookup per request', async () => {
    adminCheck.mockResolvedValue({ admin: true, role: 'moderator' });
    const cookie = seal(staffSession());
    const page = await admit('/notifications/__data.json', cookie, requireAdmin);
    const endpoint = await admit('/events/stream', cookie, requireAdmin);
    expect([page.resolved, endpoint.resolved]).toEqual([true, true]);
    expect(adminCheck.mock.calls).toEqual([['1', 'mod', 'Mod'], ['1', 'mod', 'Mod']]);
  });
});

type LookupCase = {
  name: string;
  reply: () => Promise<AdminCheck>;
  identity: AdminIdentity | null;
};

describe('requireAdmin request memo', () => {
  test.each([
    {
      name: 'an allowed lookup',
      reply: async () => ({ admin: true }),
      identity: { id: '1', login: 'mod', display_name: 'Mod', role: 'admin' }
    },
    {
      name: 'a denied lookup',
      reply: async () => ({ admin: false }),
      identity: null
    },
    {
      name: 'a failed lookup',
      reply: async () => {
        throw new Error('unavailable');
      },
      identity: null
    }
  ] satisfies LookupCase[])('shares $name for the same session', async ({ reply, identity }: LookupCase) => {
    const session = staffSession();
    adminCheck.mockImplementation(reply);
    const concurrent = await Promise.all([requireAdmin(session), requireAdmin(session)]);
    const settled = await requireAdmin(session);
    expect([...concurrent, settled]).toEqual([identity, identity, identity]);
    expect(adminCheck).toHaveBeenCalledTimes(1);
  });
});
