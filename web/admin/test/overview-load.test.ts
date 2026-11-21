// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterAll, beforeAll, beforeEach, describe, expect, mock, spyOn, test } from 'bun:test';
import { privateEnv, stubSvelteKit } from './sveltekit';
import type { AdminRole } from '../src/lib/access';
import type { Session } from '../src/lib/server/session';

stubSvelteKit();

const services = await import('../src/lib/server/services');
const giveaways = await import('../src/lib/server/giveaways');
const liveCount = await import('../src/lib/server/live-count');
const { emptyShardSnapshot } = await import('../src/lib/server/fallback');
const { load } = await import('../src/routes/(admin)/+page.server');
const { GET } = await import('../src/routes/(admin)/overview/enrollment/+server');

const STATS = {
  total_users: 9,
  active_users: 8,
  premium_users: 4,
  vip_users: 1,
  paid_users: 2,
  granted_users: 1
};
const ENROLLMENT = { days: [{ date: '2026-09-30', count: 4 }], stats: STATS };
const PROBES = [{ id: 'users', label: 'Users', ok: true, ms: 4 }];
const FEED = { total: '12', ranked: '2', entries: [{ id: '7', name: 'crumbs', count: '9' }] };
const SESSION: Session = {
  user_id: '1',
  login: 'owner',
  display_name: 'Owner',
  role: 'mod',
  iat: 0,
  expires_at: 0
};

let failing: readonly string[];
let count: number | null;
let spies: { mockRestore(): void }[];

const read = <T>(name: string, value: T) => async (): Promise<T> => {
  if (failing.includes(name)) throw new Error(`${name} unavailable`);
  return value;
};

const readEnrollment = read('enrollment', ENROLLMENT);
const userEnrollment = mock((..._args: Parameters<typeof services.userEnrollment>) => readEnrollment());

const PANELS = ['accountStats', 'enrollment', 'fleet', 'trials', 'health', 'feed', 'audit', 'bot', 'giveawayAlerts'] as const;
const previousBotId = privateEnv.TWITCH_BOT_USER_ID;

beforeAll(() => {
  privateEnv.TWITCH_BOT_USER_ID = 'bot';
  spies = [
    spyOn(services, 'adminCheck').mockResolvedValue({ admin: true, role: 'owner' }),
    spyOn(services, 'userStats').mockImplementation(read('accountStats', STATS)),
    spyOn(services, 'userEnrollment').mockImplementation(userEnrollment),
    spyOn(services, 'shardSnapshot').mockImplementation(read('fleet', { ...emptyShardSnapshot(), shard_count: 3 })),
    spyOn(services, 'trialList').mockImplementation(read('trials', { version: 1, trials: [] })),
    spyOn(services, 'serviceHealth').mockImplementation(read('health', PROBES)),
    spyOn(services, 'feedBoard').mockImplementation(read('feed', FEED)),
    spyOn(services, 'auditList').mockImplementation(read('audit', [])),
    spyOn(services, 'tokenStatus').mockImplementation(read('bot', { present: true })),
    spyOn(giveaways, 'giveawayAlerts').mockImplementation(read('giveawayAlerts', [])),
    spyOn(liveCount, 'liveUserCount').mockImplementation(() => read('liveCount', count)())
  ];
});

afterAll(() => {
  for (const spy of spies) spy.mockRestore();
  privateEnv.TWITCH_BOT_USER_ID = previousBotId;
});

beforeEach(() => {
  failing = [];
  count = 2;
  userEnrollment.mockClear();
});

function readOverview(query = '', role: AdminRole = 'owner') {
  return load({
    url: new URL(`https://admin.example/${query}`),
    parent: async () => ({ id: SESSION.user_id, role })
  } as Parameters<typeof load>[0]);
}

function getEnrollment(query: string, session: Session | null = SESSION) {
  return GET({
    locals: { session },
    url: new URL(`https://admin.example/overview/enrollment${query}`)
  } as Parameters<typeof GET>[0]);
}

describe('admin overview load', () => {
  test('streams KPI data and preserves the initial enrollment window', async () => {
    const data = await readOverview('?days=7');
    const kpis = [data.accountStats, data.liveCount, data.health];
    expect(kpis.every((kpi) => kpi instanceof Promise)).toBe(true);
    expect(await Promise.all(kpis)).toEqual([
      { ok: true, value: STATS },
      2,
      { ok: true, value: PROBES }
    ]);
    expect({
      days: data.days,
      enrollment: await data.enrollment,
      botLink: data.botLink,
      reads: userEnrollment.mock.calls
    }).toEqual({
      days: 7,
      enrollment: { ok: true, value: ENROLLMENT },
      botLink: 'https://admin.example/auth/bot/login',
      reads: [[SESSION.user_id, 7]]
    });
  });

  test.each([
    ['available', []],
    ['one failing read', ['fleet']],
    ['all failing reads', PANELS]
  ] as const)('marks each panel independently when %s', async (_case, unavailable) => {
    failing = unavailable;
    const data = await readOverview();
    const results = await Promise.all(PANELS.map((name) => data[name]));
    expect(PANELS.filter((_, index) => !results[index].ok)).toEqual([...unavailable]);
  });

  test('the selected enrollment window and other panels retain their fallback shapes', async () => {
    failing = ['enrollment', 'fleet', 'feed'];
    const data = await readOverview('?days=90');
    const [enrollment, fleet, feed] = await Promise.all([data.enrollment, data.fleet, data.feed]);
    expect({
      days: enrollment.value.days.length,
      users: enrollment.value.stats.total_users,
      shards: fleet.value.shard_count,
      feed: feed.value
    }).toEqual({ days: 90, users: 0, shards: 0, feed: { total: '0', ranked: '0', entries: [] } });
  });

  test.each([
    ['no live users', 0, [], 0],
    ['unavailable live count', null, [], null],
    ['failed live count', 2, ['liveCount'], null]
  ] as const)('keeps zero and missing live counts distinct for %s', async (_case, value, unavailable, expected) => {
    count = value;
    failing = unavailable;
    const data = await readOverview();
    expect(await data.liveCount).toBe(expected);
  });

  test.each([
    ['owner', true, false],
    ['admin', true, false],
    ['moderator', false, true]
  ] as const)('applies giveaway access for %s', async (role, canViewGiveaways, ok) => {
    failing = ['giveawayAlerts'];
    const data = await readOverview('', role);
    expect({ canViewGiveaways: data.canViewGiveaways, panel: await data.giveawayAlerts }).toEqual({
      canViewGiveaways,
      panel: { ok, value: [] }
    });
  });
});

describe('overview enrollment endpoint', () => {
  test.each([
    ['?days=7', 7],
    ['?days=30', 30],
    ['?days=90', 90],
    ['?days=1', 30],
    ['', 30]
  ] as const)('returns enrollment data for %j', async (query, expected) => {
    const response = await getEnrollment(query);
    expect({ status: response.status, enrollment: await response.json(), reads: userEnrollment.mock.calls }).toEqual({
      status: 200,
      enrollment: ENROLLMENT,
      reads: [[SESSION.user_id, expected]]
    });
  });

  test.each([
    { name: 'unauthenticated requests', session: null, unavailable: [], status: 403, reads: 0 },
    { name: 'upstream failures', session: SESSION, unavailable: ['enrollment'], status: 503, reads: 1 }
  ])('returns the endpoint error for $name', async ({ session, unavailable, status, reads }) => {
    failing = unavailable;
    await expect(getEnrollment('?days=7', session)).rejects.toMatchObject({ status });
    expect(userEnrollment.mock.calls).toHaveLength(reads);
  });
});
