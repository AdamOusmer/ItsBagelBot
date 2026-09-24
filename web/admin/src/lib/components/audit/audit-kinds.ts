// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AuditEntry } from '$lib/server/services';

export const AUDIT_KINDS = [
  'all',
  'users',
  'staff',
  'fleet',
  'config',
  'messaging',
  'other'
] as const;

export type AuditKind = (typeof AUDIT_KINDS)[number];

export const KIND_LABEL = {
  all: 'admin.audit.kindAll',
  users: 'admin.audit.kindUsers',
  staff: 'admin.audit.kindStaff',
  fleet: 'admin.audit.kindFleet',
  config: 'admin.audit.kindConfig',
  messaging: 'admin.audit.kindMessaging',
  other: 'admin.audit.kindOther'
} as const satisfies Record<AuditKind, string>;

const USER_ACTIONS = new Set([
  'ban',
  'unban',
  'delete',
  'reset',
  'clear_token',
  'set_active',
  'set_status',
  'set_creator_code',
  'impersonate',
  'restart'
]);

const PREFIX_KIND: readonly (readonly [string, AuditKind])[] = [
  ['staff_', 'staff'],
  ['shard_', 'fleet'],
  ['lane_', 'fleet'],
  ['deploy_', 'fleet'],
  ['db_credential_', 'config'],
  ['bot_counter_', 'config'],
  ['send_notification', 'messaging'],
  ['delete_notification', 'messaging']
];

export function auditKind(action: string): AuditKind {
  if (USER_ACTIONS.has(action)) return 'users';
  const hit = PREFIX_KIND.find(([prefix]) => action.startsWith(prefix));
  return hit ? hit[1] : 'other';
}

export function inKind(entry: AuditEntry, kind: AuditKind): boolean {
  return kind === 'all' || auditKind(entry.action) === kind;
}
