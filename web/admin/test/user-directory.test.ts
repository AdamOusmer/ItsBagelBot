// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { afterEach, beforeEach, expect, mock, spyOn, test } from 'bun:test';
import { stubSvelteKit } from './sveltekit';
import { EMPTY_USER_STATS } from '../src/lib/server/fallback';

stubSvelteKit();

const nats = await import('@bagel/kit/server/nats');
const { directoryQuery, loadUserDirectory } = await import('../src/lib/server/user-directory');
const { liveUserCount } = await import('../src/lib/server/live-count');

const liveSubject = 'rpc.broadcaster.live.list';
const listSubject = 'rpc.admin.user.list';
const overviewSubject = 'rpc.admin.user.overview';
const actor = 'admin';
const replies = new Map<string, unknown>();
let rpc: ReturnType<typeof spyOn<typeof nats, 'rpc'>>;

beforeEach(() => {
  replies.clear();
  rpc = spyOn(nats, 'rpc').mockImplementation(async <T>(subject: string) => {
    if (!replies.has(subject)) throw new Error(`unexpected RPC ${subject}`);
    const reply = replies.get(subject);
    if (reply instanceof Error) throw reply;
    return reply as T;
  });
});

afterEach(() => rpc.mockRestore());

function query(search: string) {
  return directoryQuery(new URL(`https://admin.example/users?${search}`));
}

test.each([26, 42])('sends page %s to the server without the old cap', async (page) => {
  replies.set(overviewSubject, { stats: EMPTY_USER_STATS, page: 26, page_size: 15, max_pages: 26, total: 381 });
  const dir = await loadUserDirectory(actor, query(`page=${page}`));
  expect(dir).toMatchObject({ page: 26, maxPages: 26, total: 381, degraded: false });
  expect(rpc.mock.calls[0]).toEqual([
    overviewSubject, { actor_id: actor, page, limit: 15, search: '', state: '', order: 'newest' }
  ]);
});

test('keeps directory rows available when an older service omits the matching total', async () => {
  const user = { id: 1, username: 'crumbs', is_active: true, status: 'free', banned: false };
  replies.set(overviewSubject, { users: [user], stats: EMPTY_USER_STATS, page: 1, max_pages: 25 });
  const dir = await loadUserDirectory(actor, query('q=crumbs'));
  expect(dir).toMatchObject({ total: null, recent: [user], degraded: false });
});

async function readDemo(search: string) {
  const env = { ...await import('$app/env') };
  mock.module('$app/env', () => ({ ...env, dev: true }));
  try {
    const directory = await import('../src/dev/server/user-directory');
    const count = await import('../src/dev/server/live-count');
    return await Promise.all([directory.loadUserDirectory(actor, query(search)), count.liveUserCount()]);
  } finally {
    mock.module('$app/env', () => env);
  }
}

test.each([
  { search: 'page=26', total: 5 },
  { search: 'page=26&live=1&state=free&q=bagel', total: 1 },
  { search: 'page=26&live=1&state=banned', total: 0 }
])('counts the matching demo users for $search', async ({ search, total }) => {
  const [dir, live] = await readDemo(search);
  expect({ page: dir.page, maxPages: dir.maxPages, total: dir.total, rows: dir.recent.length, stats: dir.stats, live }).toEqual({
    page: 1, maxPages: 1, total, rows: total, live: 2,
    stats: {
      total_users: 5, active_users: 3, premium_users: 3, paid_users: 2, vip_users: 1, granted_users: 0,
      directory_counts: { paid: 1, vip: 1, free: 1, inactive: 1, banned: 1 }
    }
  });
});

test.each([
  { label: 'live set failed', live: new Error('unavailable'), list: {}, total: null, callsList: false },
  { label: 'users list failed', live: { ids: [10, 20] }, list: new Error('unavailable'), total: null, callsList: true },
  { label: 'users list omitted total', live: { ids: [10, 20] }, list: {}, total: null, callsList: true },
  { label: 'no registered users live', live: { ids: [10, 20] }, list: { total: 0 }, total: 0, callsList: true },
  { label: 'one registered user live', live: { ids: [10, 20] }, list: { total: 1 }, total: 1, callsList: true }
])('reads the registered live count when $label', async ({ live, list, total, callsList }) => {
  replies.set(liveSubject, live);
  replies.set(listSubject, list);
  expect(await liveUserCount(actor)).toBe(total);
  const request: Parameters<typeof nats.rpc> = [
    listSubject, { actor_id: actor, page: 1, limit: 1, only_live: true, live_ids: [10, 20] }, 2_000
  ];
  expect(rpc.mock.calls.slice(1)).toEqual(callsList ? [request] : []);
});

test('shares the live set with the directory while both reads are in flight', async () => {
  replies.set(liveSubject, { ids: [10, 20] });
  replies.set(listSubject, { total: 1 });
  replies.set(overviewSubject, { stats: EMPTY_USER_STATS, page: 1, page_size: 15, max_pages: 1, total: 1 });
  const [dir, count] = await Promise.all([loadUserDirectory(actor, query('live=1')), liveUserCount(actor)]);
  replies.set(liveSubject, { ids: [] });
  replies.set(listSubject, { total: 0 });
  const nextCount = await liveUserCount(actor);
  expect({
    dir: dir.total, count, nextCount, scans: rpc.mock.calls.filter(([subject]) => subject === liveSubject).length
  }).toEqual({ dir: 1, count: 1, nextCount: 0, scans: 2 });
});
