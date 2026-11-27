// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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

export async function loadUserDirectory(actorId: string, q: DirectoryQuery): Promise<UserDirectory> {
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
