// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { STAFF_RANK as RANK, type StaffRole } from '@bagel/kit/staff-role';

export type AdminRole = StaffRole;

export function isManager(role: AdminRole): boolean {
  return role === 'admin' || role === 'owner';
}

// For ingress, lanes, notifications and loyalty counters this table is the only enforcement.
export const ROLE_FOR = {
  'users.read': 'moderator',
  'users.ban': 'moderator',
  'users.grant': 'admin',
  'users.test': 'admin',
  'users.token': 'admin',
  'users.delete': 'owner',
  'users.impersonate': 'admin',
  'users.restart': 'admin',
  'shards.scale': 'admin',
  'trials.manage': 'admin',
  'lanes.mutate': 'admin',
  'notifications.send': 'admin',
  'giveaways.manage': 'admin',
  'counters.manage': 'owner',
  'staff.manage': 'admin',
  'audit.read': 'admin',
  'secrets.manage': 'owner',
  'deploys.manage': 'owner',
  'bot.token': 'owner'
} as const satisfies Record<string, AdminRole>;

export type AccessKey = keyof typeof ROLE_FOR;

export function allows(role: AdminRole, key: AccessKey): boolean {
  return RANK[role] >= RANK[ROLE_FOR[key]];
}

export function canManage(actor: AdminRole, target: AdminRole): boolean {
  if (!isManager(actor)) return false;
  if (target === 'owner') return actor === 'owner';
  return RANK[actor] >= RANK[target];
}

export function grantableRoles(actor: AdminRole): AdminRole[] {
  const all: AdminRole[] = ['moderator', 'admin', 'owner'];
  return all.filter((target) => canManage(actor, target));
}
