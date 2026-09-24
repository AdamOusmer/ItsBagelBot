// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AdminUserWire } from '$lib/server/services';

export type UserState = 'banned' | 'inactive' | 'vip' | 'paid' | 'free';

export function stateOf(u: AdminUserWire): UserState {
  if (u.banned) return 'banned';
  if (!u.is_active) return 'inactive';
  return (u.status as UserState) ?? 'free';
}

export const USER_STATES = ['all', 'vip', 'paid', 'free', 'banned', 'inactive'] as const;

export type UserStateFilter = (typeof USER_STATES)[number];

export const USER_STATE_LABEL = {
  all: 'admin.users.stateAll',
  vip: 'admin.users.stateVip',
  paid: 'admin.users.statePaid',
  free: 'admin.users.stateFree',
  banned: 'admin.users.stateBanned',
  inactive: 'admin.users.stateInactive'
} as const satisfies Record<UserStateFilter, string>;
