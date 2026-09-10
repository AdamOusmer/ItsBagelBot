// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AdminUserWire } from '$lib/server/services';

// One effective state per user, five colours.
// Precedence: banned beats inactive beats tier. Tags still show every flag; the
// pill and the filter use the effective state.
//
// The same precedence is spelled out server-side in the directory filter
// (`matchesState`) and in the users service. It lives here as well because the
// row's colour has to agree with the chip the filter offers, and when the rule
// was inline in the template a banned-and-inactive user rendered inactive in
// the list while the `?state=banned` filter still returned them.
export type UserState = 'banned' | 'inactive' | 'vip' | 'paid' | 'free';

export function stateOf(u: AdminUserWire): UserState {
  if (u.banned) return 'banned';
  if (!u.is_active) return 'inactive';
  return (u.status as UserState) ?? 'free';
}

/** The filter values the toolbar offers, in the order it shows them. */
export const USER_STATES = ['all', 'vip', 'paid', 'free', 'banned', 'inactive'] as const;

export type UserStateFilter = (typeof USER_STATES)[number];

// Keyed off the catalog so a renamed key fails the type check rather than
// rendering the raw dot-path.
export const USER_STATE_LABEL = {
  all: 'admin.users.stateAll',
  vip: 'admin.users.stateVip',
  paid: 'admin.users.statePaid',
  free: 'admin.users.stateFree',
  banned: 'admin.users.stateBanned',
  inactive: 'admin.users.stateInactive'
} as const satisfies Record<UserStateFilter, string>;
