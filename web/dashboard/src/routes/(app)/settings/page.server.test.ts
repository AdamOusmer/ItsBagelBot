// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, describe, expect, jest, mock, test } from 'bun:test';
import { STATIC_LOCALES, staticText } from '../../../../../kit/lib/i18n/static';
import { privateEnv, stubSvelteKit } from '../../../../test/sveltekit';

stubSvelteKit();

type Session = { user_id: string; delegate_of?: string; impersonator_id?: string };

const hiddenWrites: { page: string; userId: string; hidden: boolean }[] = [];
type PurgeBody = { files: string[] } | { tags: string[] };
const purgeBodies: PurgeBody[] = [];
let writeFails = false;
let purgeStatus = 200;
let savedLocale = 'en';

const services = await import('../../../lib/server/services');
const servicesMock = () => ({
  ...services,
  delegationList: async () => [],
  delegationAccess: async () => [],
  notificationsForUser: async () => ({ notifications: [] }),
  hasGrant: async () => true,
  userCommandsPage: async () => true,
  userLeaderboardPage: async () => true,
  userLocale: async () => savedLocale,
  accountState: async () => ({ username: 'StreamerLogin' }),
  publishEventSub: async () => {},
  deleteSelf: async () => {},
  auditDashboardImpersonation: () => {},
  setCommandsPage: async (userId: string, hidden: boolean) => {
    if (writeFails) throw new Error('rpc down');
    hiddenWrites.push({ page: 'commands_page', userId, hidden });
  },
  setLeaderboardPage: async (userId: string, hidden: boolean) => {
    if (writeFails) throw new Error('rpc down');
    hiddenWrites.push({ page: 'leaderboard_page', userId, hidden });
  }
});
mock.module('$lib/server/services', servicesMock);
mock.module('../../../lib/server/services', servicesMock);
const fetchesStore = await import('../../../lib/server/fetches-store');
mock.module('$lib/server/fetches-store', () => ({ ...fetchesStore, listFetches: async () => ({ defs: [], keys: [] }) }));

const { channelPageUrls } = await import('../../../lib/server/channel-urls');
const { actions, load } = await import('./+page.server');

const realFetch = globalThis.fetch;

beforeEach(() => {
  hiddenWrites.length = 0;
  purgeBodies.length = 0;
  writeFails = false;
  purgeStatus = 200;
  Object.assign(privateEnv, { CF_ZONE_ID: 'zone', CF_CACHE_PURGE_TOKEN: 'token' });
  globalThis.fetch = (async (_url: string, init: { body: string }) => {
    purgeBodies.push(JSON.parse(init.body));
    return new Response(null, { status: purgeStatus });
  }) as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = realFetch;
});

type Toggle = { action: 'setCommandsPage' | 'setLeaderboardPage'; page: 'commands_page' | 'leaderboard_page' };

function submitToggle(toggle: Toggle, session: Session | null, fields: Record<string, string>, locale: 'en' | 'fr' = 'en') {
  const request = new Request(`https://dashboard.itsbagelbot.com/settings?/${toggle.action}`, {
    method: 'POST',
    headers: { 'content-type': 'application/x-www-form-urlencoded' },
    body: new URLSearchParams(fields).toString()
  });
  return actions[toggle.action]({ request, locals: { session, locale } } as never);
}

const channelPurge: PurgeBody[] = [{ files: channelPageUrls('7', ['streamerlogin']) }, { tags: ['lb:streamerlogin'] }];
const refused = { status: 403, data: { error: staticText('en', 'serverErrors.notAllowed') } };

const cases: {
  name: string;
  session: Session | null;
  fields: Record<string, string>;
  locale?: 'en' | 'fr';
  writeFails?: boolean;
  purgeStatus?: number;
  want: { result: (page: string) => unknown; writes: [string, boolean][]; purges: PurgeBody[] };
}[] = [
  {
    name: 'a French refusal preserves the authorization status and localizes its message',
    session: null,
    fields: {},
    locale: 'fr',
    want: { result: () => ({ status: 403, data: { error: staticText('fr', 'serverErrors.notAllowed') } }), writes: [], purges: [] }
  },
  {
    name: 'a delegate session is refused',
    session: { user_id: '1', delegate_of: '2' },
    fields: { enabled: 'on' },
    want: { result: () => refused, writes: [], purges: [] }
  },
  {
    name: 'an admin "view as" session is refused',
    session: { user_id: '1', impersonator_id: '9' },
    fields: { enabled: 'on' },
    want: { result: () => refused, writes: [], purges: [] }
  },
  {
    name: 'owner enabled=on writes hidden=false then purges the commands page URLs and the leaderboard tag',
    session: { user_id: '7' },
    fields: { enabled: 'on' },
    want: { result: (page) => ({ ok: true, action: page, edgeDelayed: false }), writes: [['7', false]], purges: channelPurge }
  },
  {
    name: 'a failed purge surfaces edgeDelayed: true without failing the request',
    session: { user_id: '7' },
    fields: { enabled: 'on' },
    purgeStatus: 500,
    want: { result: (page) => ({ ok: true, action: page, edgeDelayed: true }), writes: [['7', false]], purges: channelPurge }
  },
  {
    name: 'enabled absent (unchecked switch) writes hidden=true',
    session: { user_id: '7' },
    fields: {},
    want: { result: (page) => ({ ok: true, action: page, edgeDelayed: false }), writes: [['7', true]], purges: channelPurge }
  },
  {
    name: 'a failed write answers 502 and purges nothing',
    session: { user_id: '7' },
    fields: { enabled: 'on' },
    writeFails: true,
    want: {
      result: () => ({ status: 502, data: { error: staticText('en', 'serverErrors.updateRetry') } }),
      writes: [],
      purges: []
    }
  }
];

const toggles: Toggle[] = [
  { action: 'setCommandsPage', page: 'commands_page' },
  { action: 'setLeaderboardPage', page: 'leaderboard_page' }
];

describe.each(toggles)('$action action', (toggle) => {
  test.each(cases)('$name', async ({ session, fields, locale, writeFails: fails, purgeStatus: status, want }) => {
    writeFails = fails ?? false;
    purgeStatus = status ?? 200;
    const result = await submitToggle(toggle, session, fields, locale);
    const expected = {
      result: want.result(toggle.page),
      writes: want.writes.map(([userId, hidden]) => ({ page: toggle.page, userId, hidden })),
      purges: want.purges
    };
    expect({ result, writes: hiddenWrites, purges: purgeBodies } as unknown).toEqual(expected);
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
