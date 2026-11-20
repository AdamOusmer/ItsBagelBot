// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, describe, expect, jest, mock, spyOn, test } from 'bun:test';
import { logger } from '@bagel/kit/server/logger';
import { STATIC_LOCALES, staticText } from '../../../../../kit/lib/i18n/static';
import { privateEnv, stubSvelteKit } from '../../../../test/sveltekit';
import { PAGE_ACTION, PUBLIC_PAGES, pageScope, type PublicPage } from '../../../lib/public-pages';

stubSvelteKit();

type Session = { user_id: string; delegate_of?: string; impersonator_id?: string };

const hiddenWrites: { page: string; userId: string; hidden: boolean }[] = [];
const marketingWrites: unknown[] = [];
type PurgeBody = { files: string[] } | { tags: string[] };
const purgeBodies: PurgeBody[] = [];
const audits: { action: string; detail: string }[] = [];
let writeFails = false;
let purgeStatus = 200;
let savedLocale = 'en';
const hiddenPages = new Set<PublicPage>();
let failingPageRead: PublicPage | null = null;

const services = await import('../../../lib/server/services');
const servicesMock = () => ({
  ...services,
  delegationList: async () => [],
  delegationAccess: async () => [],
  notificationsForUser: async () => ({ notifications: [] }),
  hasGrant: async () => true,
  userLocale: async () => savedLocale,
  accountState: async () => ({ username: 'StreamerLogin' }),
  publishEventSub: async () => {},
  deleteSelf: async () => {},
  setMarketing: async (...args: unknown[]) => {
    marketingWrites.push(args);
    if (writeFails) throw new Error('rpc down');
  },
  auditDashboardImpersonation: (session: Session, action: string, detail: string) => {
    if (session.impersonator_id) audits.push({ action, detail });
  },
  pageToggles: Object.fromEntries(
    PUBLIC_PAGES.map((page) => [
      page,
      {
        read: async () => {
          if (page === failingPageRead) throw new Error('rpc down');
          return !hiddenPages.has(page);
        },
        write: async (userId: string, hidden: boolean) => {
          if (writeFails) throw new Error('rpc down');
          hiddenWrites.push({ page: pageScope(page), userId, hidden });
        }
      }
    ])
  )
});
mock.module('#lib/server/services', servicesMock);
mock.module('../../../lib/server/services', servicesMock);
const fetchesStore = await import('../../../lib/server/fetches-store');
mock.module('#lib/server/fetches-store', () => ({ ...fetchesStore, listFetches: async () => ({ defs: [], keys: [] }) }));

const { allPageUrls } = await import('../../../lib/server/channel-urls');
const { actions, load } = await import('./+page.server');

const realFetch = globalThis.fetch;

beforeEach(() => {
  hiddenWrites.length = 0;
  marketingWrites.length = 0;
  purgeBodies.length = 0;
  audits.length = 0;
  writeFails = false;
  purgeStatus = 200;
  hiddenPages.clear();
  failingPageRead = null;
  Object.assign(privateEnv, { CF_ZONE_ID: 'zone', CF_CACHE_PURGE_TOKEN: 'token' });
  globalThis.fetch = (async (_url: string, init: { body: string }) => {
    purgeBodies.push(JSON.parse(init.body));
    return new Response(null, { status: purgeStatus });
  }) as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = realFetch;
});

type Toggle = { action: (typeof PAGE_ACTION)[PublicPage]; page: string };

function submitToggle(toggle: Toggle, session: Session | null, fields: Record<string, string>, locale: 'en' | 'fr' = 'en') {
  const request = new Request(`https://dashboard.itsbagelbot.com/settings?/${toggle.action}`, {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams(fields).toString()
  });
  return actions[toggle.action]({ request, locals: { session, locale } } as never);
}

const channelPurge: PurgeBody[] = [{ files: allPageUrls('7', ['streamerlogin']) }, { tags: ['lb:streamerlogin'] }];
const refused = { status: 403, data: { error: staticText('en', 'serverErrors.notAllowed') } };
const mailingAddress = 'test mailing address';

test.each([
  { name: 'owner opts in with a mailing address', session: { user_id: '7' }, status: 'granted', address: mailingAddress, want: { ok: true }, writes: true },
  { name: 'owner withdraws', session: { user_id: '7' }, status: 'withdrawn', want: { ok: true }, writes: true },
  { name: 'owner cannot opt in without a mailing address', session: { user_id: '7' }, status: 'granted', want: { status: 503 }, writes: false },
  { name: 'blank mailing address is treated as unset', session: { user_id: '7' }, status: 'granted', address: '   ', want: { status: 503 }, writes: false },
  { name: 'unavailable write', session: { user_id: '7' }, status: 'granted', address: mailingAddress, want: { status: 502 }, writes: true, failed: true },
  { name: 'invalid status', session: { user_id: '7' }, status: 'none', want: { status: 400 }, writes: false },
  { name: 'signed out', session: null, status: 'granted', want: { status: 401 }, writes: false },
  { name: 'delegate', session: { user_id: '7', delegate_of: '8' }, status: 'granted', want: { status: 403 }, writes: false },
  { name: 'impersonation', session: { user_id: '7', impersonator_id: '9' }, status: 'withdrawn', want: { status: 403 }, writes: false }
])('marketing preference: $name', async ({ session, status, address, want, writes, failed }) => {
  privateEnv.BUSINESS_MAILING_ADDRESS = address;
  writeFails = failed ?? false;
  const request = new Request('https://dashboard.itsbagelbot.com/settings?/marketing', {
    method: 'POST', body: new URLSearchParams({ status })
  });
  const result = await actions.marketing({ request, locals: { session, locale: 'fr' } } as never);
  expect(result).toMatchObject(want);
  expect(marketingWrites).toEqual(writes ? [['7', { status, locale: 'fr' }]] : []);
});

const cases: {
  name: string;
  session: Session | null;
  fields: Record<string, string>;
  locale?: 'en' | 'fr';
  writeFails?: boolean;
  purgeStatus?: number;
  want: { result: (page: string) => unknown; writes: [string, boolean][]; purged: boolean };
}[] = [
  {
    name: 'a French refusal preserves the authorization status and localizes its message',
    session: null,
    fields: {},
    locale: 'fr',
    want: { result: () => ({ status: 403, data: { error: staticText('fr', 'serverErrors.notAllowed') } }), writes: [], purged: false }
  },
  {
    name: 'a delegate session is refused',
    session: { user_id: '1', delegate_of: '2' },
    fields: { enabled: 'on' },
    want: { result: () => refused, writes: [], purged: false }
  },
  {
    name: 'owner enabled=on writes hidden=false then purges that page from the edge',
    session: { user_id: '7' },
    fields: { enabled: 'on' },
    want: { result: (page) => ({ ok: true, action: page, edgeDelayed: false }), writes: [['7', false]], purged: true }
  },
  {
    name: 'a failed purge surfaces edgeDelayed: true without failing the request',
    session: { user_id: '7' },
    fields: { enabled: 'on' },
    purgeStatus: 500,
    want: { result: (page) => ({ ok: true, action: page, edgeDelayed: true }), writes: [['7', false]], purged: true }
  },
  {
    name: 'enabled absent (unchecked switch) writes hidden=true',
    session: { user_id: '7' },
    fields: {},
    want: { result: (page) => ({ ok: true, action: page, edgeDelayed: false }), writes: [['7', true]], purged: true }
  },
  {
    name: 'a failed write answers 502 and purges nothing',
    session: { user_id: '7' },
    fields: { enabled: 'on' },
    writeFails: true,
    want: {
      result: () => ({ status: 502, data: { error: staticText('en', 'serverErrors.updateRetry') } }),
      writes: [],
      purged: false
    }
  }
];

const toggles: Toggle[] = PUBLIC_PAGES.map((page) => ({ action: PAGE_ACTION[page], page: pageScope(page) }));

describe.each(toggles)('$action action', (toggle) => {
  test.each(cases)('$name', async ({ session, fields, locale, writeFails: fails, purgeStatus: status, want }) => {
    writeFails = fails ?? false;
    purgeStatus = status ?? 200;
    const result = await submitToggle(toggle, session, fields, locale);
    const expected = {
      result: want.result(toggle.page),
      writes: want.writes.map(([userId, hidden]) => ({ page: toggle.page, userId, hidden })),
      purges: want.purged ? channelPurge : []
    };
    expect({ result, writes: hiddenWrites, purges: purgeBodies } as unknown).toEqual(expected);
  });
});

test('a failed page toggle logs the upstream error behind its 502', async () => {
  writeFails = true;
  const logged = spyOn(logger, 'error').mockImplementation(() => {});
  const toggle = toggles[0]!;

  await submitToggle(toggle, { user_id: '7' }, { enabled: 'on' });

  expect(logged.mock.calls).toEqual([[{ err: new Error('rpc down') }, `[settings] ${toggle.page}:set failed`]]);
  logged.mockRestore();
});

test.each(toggles)('$action lets an admin view-as session flip the channel page and audits it', async (toggle) => {
  const result = await submitToggle(toggle, { user_id: '7', impersonator_id: '9' }, {});
  expect({ result, writes: hiddenWrites, purges: purgeBodies, audits } as unknown).toEqual({
    result: { ok: true, action: toggle.page, edgeDelayed: false },
    writes: [{ page: toggle.page, userId: '7', hidden: true }],
    purges: channelPurge,
    audits: [{ action: `${toggle.page}:set`, detail: 'enabled=false' }]
  });
});

test('deleting the account purges its commands pages and its leaderboard tag', async () => {
  jest.useFakeTimers();
  try {
    const cookies = { set: () => {}, delete: () => {} };
    const url = new URL('https://dashboard.itsbagelbot.com/settings?/delete');
    const locals = { session: { user_id: '7', login: 'StreamerLogin' }, locale: 'en' };
    const thrown = await Promise.resolve(actions.delete({ locals, cookies, url } as never)).catch((e: { status: number }) => e.status);
    expect(thrown).toBe(302);
    for (let i = 0; i < 3; i++) {
      await Promise.resolve();
      jest.advanceTimersByTime(10_000);
    }
  } finally {
    jest.useRealTimers();
  }
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(purgeBodies).toEqual(channelPurge);
});

test('settings preserves every saved language as the selected one', async () => {
  const saved: string[] = [];
  for (const locale of STATIC_LOCALES) {
    savedLocale = locale;
    const data = (await load({ locals: { session: { user_id: '42' }, locale: 'en' } } as never)) as { savedLocale: string };
    saved.push(data.savedLocale);
  }
  expect(saved).toEqual([...STATIC_LOCALES]);
});

test.each([
  { name: 'owner with an address configured', session: { user_id: '42' }, status: 'withdrawn', address: `  ${mailingAddress}  `, want: { status: 'withdrawn', mailingAddress } },
  { name: 'subscribed owner without an address configured', session: { user_id: '42' }, status: 'granted', want: { status: 'granted', mailingAddress: '' } },
  { name: 'impersonation', session: { user_id: '42', impersonator_id: '9' }, status: 'granted', address: mailingAddress, want: null }
])('settings email preference data: $name', async ({ session, status, address, want }) => {
  privateEnv.BUSINESS_MAILING_ADDRESS = address;
  const locals = { session, locale: 'en', accountState: { value: { marketingStatus: status } } };
  expect(await load({ locals } as never)).toMatchObject({ emailPreferences: want });
});

test('settings reports every public page flag and degrades on a failed read without hiding the page', async () => {
  const read = async () =>
    (await load({ locals: { session: { user_id: '42' }, locale: 'en' } } as never)) as unknown as { pages: Record<string, boolean>; degraded: boolean };

  hiddenPages.add('songqueue');
  expect(await read()).toMatchObject({ pages: { commands: true, leaderboard: true, songqueue: false }, degraded: false });

  hiddenPages.clear();
  failingPageRead = 'leaderboard';
  expect(await read()).toMatchObject({ pages: { commands: true, leaderboard: true, songqueue: true }, degraded: true });
});
