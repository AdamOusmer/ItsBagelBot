// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { stateOf } from '#lib/components/users/user-state';
import { EMPTY_USER_STATS } from '#lib/server/fallback';
import { USER_PAGE_SIZE, type AdminUserWire, type DirectoryStats, type UserOrder } from '#lib/server/services';
import type { DirectoryQuery, UserDirectory } from '#lib/server/user-directory';
import { sampleLiveUserIDs, sampleUsers } from '../fixtures';

export * from '#lib/server/user-directory';

function matchesSearch(user: AdminUserWire, search: string): boolean {
  if (!search) return true;
  const q = search.toLowerCase();
  return user.username.toLowerCase().includes(q) || String(user.id).includes(q);
}

function byJoined(order: UserOrder) {
  const sign = order === 'oldest' ? 1 : -1;
  return (a: AdminUserWire, b: AdminUserWire) => sign * (a.created_at ?? '').localeCompare(b.created_at ?? '');
}

function kept(user: AdminUserWire, q: DirectoryQuery): boolean {
  if (!matchesSearch(user, q.search)) return false;
  if (q.state && stateOf(user) !== q.state) return false;
  return !q.live || sampleLiveUserIDs.has(user.id);
}

function directoryStats(users: AdminUserWire[]): DirectoryStats {
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

export async function loadUserDirectory(_actorId: string, q: DirectoryQuery): Promise<UserDirectory> {
  const filtered = sampleUsers.filter((user) => kept(user, q)).sort(byJoined(q.order));
  const total = filtered.length;
  const maxPages = Math.max(1, Math.ceil(total / USER_PAGE_SIZE));
  const page = Math.min(q.page, maxPages);
  const start = (page - 1) * USER_PAGE_SIZE;
  return {
    recent: filtered.slice(start, start + USER_PAGE_SIZE),
    stats: directoryStats(sampleUsers),
    page,
    pageSize: USER_PAGE_SIZE,
    maxPages,
    hasMore: page < maxPages,
    total,
    degraded: false
  };
}
