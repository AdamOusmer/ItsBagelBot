// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { dev } from '$app/env';
import { EMPTY_USER_STATS } from '#lib/server/fallback';
import { currentLiveBroadcasterIDs } from '#lib/server/live-broadcasters';
import { parsePage, normalizeSearch } from '#lib/server/paging';
import {
  userOverview,
  USER_MAX_PAGES,
  USER_PAGE_SIZE,
  type AdminUserWire,
  type UserDirectoryQuery,
  type UserOrder
} from '#lib/server/services';
import type { UserStats } from '@bagel/kit';

const DEMO = dev && process.env.DEMO === '1';

const STATES = new Set(['vip', 'paid', 'free', 'banned', 'inactive']);

export type DirectoryQuery = UserDirectoryQuery;

export type UserDirectory = {
  recent: AdminUserWire[];
  stats: UserStats;
  page: number;
  pageSize: number;
  maxPages: number;
  hasMore: boolean;
  degraded: boolean;
};

type DemoDirectoryFixtures = {
  sampleUsers: AdminUserWire[];
  sampleStats: UserStats;
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
    page: parsePage(url.searchParams.get('page'), USER_MAX_PAGES),
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

function matchesState(user: AdminUserWire, state: string): boolean {
  if (!state) return true;
  if (user.banned) return state === 'banned';
  if (!user.is_active) return state === 'inactive';
  return user.status === state;
}

function byJoined(order: UserOrder) {
  const sign = order === 'oldest' ? 1 : -1;
  return (a: AdminUserWire, b: AdminUserWire) => sign * (a.created_at ?? '').localeCompare(b.created_at ?? '');
}

function demoPage(q: DirectoryQuery, fixtures: DemoDirectoryFixtures) {
  const filtered = fixtures.sampleUsers
    .filter((user) => kept(user, q, fixtures.sampleLiveUserIDs))
    .sort(byJoined(q.order));
  const start = (q.page - 1) * USER_PAGE_SIZE;
  const users = filtered.slice(start, start + USER_PAGE_SIZE);
  const cappedTotal = Math.min(filtered.length, USER_PAGE_SIZE * USER_MAX_PAGES);
  return {
    recent: users,
    stats: fixtures.sampleStats,
    page: q.page,
    pageSize: USER_PAGE_SIZE,
    maxPages: USER_MAX_PAGES,
    hasMore: start + USER_PAGE_SIZE < cappedTotal,
    search: q.search,
    degraded: false
  };
}

function kept(user: AdminUserWire, q: DirectoryQuery, liveIDs: ReadonlySet<number>): boolean {
  if (!matchesSearch(user, q.search)) return false;
  if (!matchesState(user, q.state)) return false;
  if (!q.live) return true;
  return liveIDs.has(user.id);
}

function degradedDirectory(q: DirectoryQuery): UserDirectory {
  return {
    recent: [],
    stats: { ...EMPTY_USER_STATS },
    page: q.page,
    pageSize: USER_PAGE_SIZE,
    maxPages: USER_MAX_PAGES,
    hasMore: false,
    degraded: true
  };
}

async function loadDirectory(actorId: string, q: DirectoryQuery): Promise<UserDirectory> {
  try {
    const liveIDs = await liveIDsFor(q);
    if (q.live && !liveIDs) return degradedDirectory(q);
    const overview = await userOverview({ actorId, ...q, liveIDs });
    return {
      recent: overview.users,
      stats: overview.stats,
      page: overview.page,
      pageSize: overview.page_size,
      maxPages: overview.max_pages,
      hasMore: overview.has_more,
      degraded: false
    };
  } catch {
    return degradedDirectory(q);
  }
}

async function liveIDsFor(q: DirectoryQuery): Promise<number[] | undefined> {
  if (!q.live) return undefined;
  const ids = await currentLiveBroadcasterIDs();
  return ids ?? undefined;
}

export function loadUserDirectory(actorId: string, q: DirectoryQuery): Promise<UserDirectory> {
  if (DEMO) return import('#lib/server/demo-data').then((fixtures) => demoPage(q, fixtures));
  return loadDirectory(actorId, q);
}
