// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AdminUserWire } from '#lib/server/services';

export const USER_TIERS = ['free', 'paid', 'vip'] as const;
export type UserTier = (typeof USER_TIERS)[number];
export type UserState = 'banned' | 'inactive' | UserTier;

export function tierOf({ status }: Pick<AdminUserWire, 'status'>): UserTier {
  return status === 'paid' || status === 'vip' ? status : 'free';
}

export function stateOf(user: AdminUserWire): UserState {
  if (user.banned) return 'banned';
  if (!user.is_active) return 'inactive';
  return tierOf(user);
}

export const USER_BADGE_LABEL = {
  free: 'admin.users.stateBadgeFree',
  paid: 'admin.users.stateBadgePaid',
  vip: 'admin.users.stateVip',
  banned: 'admin.users.stateBadgeBanned',
  inactive: 'admin.users.stateBadgeInactive'
} as const satisfies Record<UserState, string>;

export const USER_STATES = ['all', 'paid', 'vip', 'free', 'inactive', 'banned'] as const;

export type UserStateFilter = (typeof USER_STATES)[number];

export const USER_STATE_LABEL = {
  all: 'admin.users.stateAll',
  vip: 'admin.users.stateVip',
  paid: 'admin.users.statePaid',
  free: 'admin.users.stateFree',
  banned: 'admin.users.stateBanned',
  inactive: 'admin.users.stateInactive'
} as const satisfies Record<UserStateFilter, string>;
