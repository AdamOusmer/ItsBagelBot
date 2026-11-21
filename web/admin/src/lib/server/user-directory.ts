// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { stateOf } from '#lib/components/users/user-state';
import { EMPTY_USER_STATS } from '#lib/server/fallback';
import { currentLiveBroadcasterIDs } from '#lib/server/live-broadcasters';
import { parsePage, normalizeSearch } from '#lib/server/paging';
import {
  userOverview,
  USER_PAGE_SIZE,
  type AdminUserWire,
  type DirectoryStats,
  type UserDirectoryQuery,
  type UserOrder
} from '#lib/server/services';

const DEMO = dev && process.env.DEMO === '1';

const STATES = new Set(['vip', 'paid', 'free', 'banned', 'inactive']);

export type DirectoryQuery = UserDirectoryQuery;

export type UserDirectory = {
  recent: AdminUserWire[];
  stats: DirectoryStats;
  page: number;
  pageSize: number;
  maxPages: number;
  hasMore: boolean;
  total: number | null;
  degraded: boolean;
};

type DemoDirectoryFixtures = {
  sampleUsers: AdminUserWire[];
  sampleLiveUserIDs: ReadonlySet<number>;
};

function parseState(raw: string | null): string {
  const state = (raw ?? '').trim();
  return STATES.has(state) ? state : '';
}

function parseOrder(raw: string | null): UserOrder {
  return raw === 'oldest' ? 'oldest' : 'newest';
}

function parseLive(raw: string | null): boolean {
  return raw === '1';
}

export function directoryQuery(url: URL): DirectoryQuery {
  return {
    page: parsePage(url.searchParams.get('page'), Number.MAX_SAFE_INTEGER),
    search: normalizeSearch(url.searchParams.get('q')),
    state: parseState(url.searchParams.get('state')),
    order: parseOrder(url.searchParams.get('order')),
    live: parseLive(url.searchParams.get('live'))
  };
}

function matchesSearch(user: AdminUserWire, search: string): boolean {
  if (!search) return true;
  const q = search.toLowerCase();
  return user.username.toLowerCase().includes(q) || String(user.id).includes(q);
}

function byJoined(order: UserOrder) {
  const sign = order === 'oldest' ? 1 : -1;
  return (a: AdminUserWire, b: AdminUserWire) => sign * (a.created_at ?? '').localeCompare(b.created_at ?? '');
}

function demoStats(users: AdminUserWire[]): DirectoryStats {
  const counts = { paid: 0, vip: 0, free: 0, inactive: 0, banned: 0 };
  const stats = { ...EMPTY_USER_STATS, total_users: users.length, directory_counts: counts };
  for (const user of users) {
    counts[stateOf(user)]++;
    stats.active_users += Number(user.is_active);
    stats.paid_users += Number(user.status === 'paid');
    stats.vip_users += Number(user.status === 'vip');
    stats.granted_users += Number(user.status === 'paid' && ['admin', 'giveaway'].includes(user.subscription_source ?? ''));
  }
  stats.premium_users = stats.paid_users + stats.vip_users;
  stats.paid_users -= stats.granted_users;
  return stats;
}

function demoPage(q: DirectoryQuery, fixtures: DemoDirectoryFixtures): UserDirectory {
  const filtered = fixtures.sampleUsers
    .filter((user) => kept(user, q, fixtures.sampleLiveUserIDs))
    .sort(byJoined(q.order));
  const total = filtered.length;
  const maxPages = Math.max(1, Math.ceil(total / USER_PAGE_SIZE));
  const page = Math.min(q.page, maxPages);
  const start = (page - 1) * USER_PAGE_SIZE;
  return {
    recent: filtered.slice(start, start + USER_PAGE_SIZE),
    stats: demoStats(fixtures.sampleUsers),
    page,
    pageSize: USER_PAGE_SIZE,
    maxPages,
    hasMore: page < maxPages,
    total,
    degraded: false
  };
}

function kept(user: AdminUserWire, q: DirectoryQuery, liveIDs: ReadonlySet<number>): boolean {
  if (!matchesSearch(user, q.search)) return false;
  if (q.state && stateOf(user) !== q.state) return false;
  if (!q.live) return true;
  return liveIDs.has(user.id);
}

function degradedDirectory(): UserDirectory {
  return {
    recent: [],
    stats: { ...EMPTY_USER_STATS },
    page: 1,
    pageSize: USER_PAGE_SIZE,
    maxPages: 1,
    hasMore: false,
    total: null,
    degraded: true
  };
}

async function loadDirectory(actorId: string, q: DirectoryQuery): Promise<UserDirectory> {
  try {
    const liveIDs = q.live ? await currentLiveBroadcasterIDs() : undefined;
    if (liveIDs === null) return degradedDirectory();
    const overview = await userOverview({ actorId, ...q, liveIDs });
    return {
      recent: overview.users,
      stats: overview.stats,
      page: overview.page,
      pageSize: overview.page_size,
      maxPages: overview.max_pages,
      hasMore: overview.has_more,
      total: overview.total,
      degraded: false
    };
  } catch {
    return degradedDirectory();
  }
}

export function loadUserDirectory(actorId: string, q: DirectoryQuery): Promise<UserDirectory> {
  if (DEMO) return import('#lib/server/demo-data').then((fixtures) => demoPage(q, fixtures));
  return loadDirectory(actorId, q);
}
