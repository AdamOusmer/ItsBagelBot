// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { AuditEntry } from '$lib/server/services';

/**
 * The audit trail's action vocabulary, grouped for the filter.
 *
 * A classification, not a schema: `action` is a free string written by whichever
 * console action produced the row, and rows from a service that never asked this
 * file exist. Anything unrecognised lands in `other` -- which is a VISIBLE
 * segment on purpose. Folding unknowns into a named bucket would let a new
 * verb silently vanish from every filter but "all", and an audit trail that
 * hides rows is worse than one with a clumsy filter.
 */
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

/** Kind -> catalog key for the filter's visible label. */
export const KIND_LABEL = {
  all: 'admin.audit.kindAll',
  users: 'admin.audit.kindUsers',
  staff: 'admin.audit.kindStaff',
  fleet: 'admin.audit.kindFleet',
  config: 'admin.audit.kindConfig',
  messaging: 'admin.audit.kindMessaging',
  other: 'admin.audit.kindOther'
} as const satisfies Record<AuditKind, string>;

// The per-user verbs, spelled out rather than prefix-matched: they are bare
// words ('ban', 'delete', 'restart') with no shared prefix to match on, and
// guessing from the target would misfile a row whose target happens to be
// numeric for another reason.
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

// Everything else groups by the prefix its writer already uses. Order matters
// only in that the first match wins; no two prefixes overlap today.
const PREFIX_KIND: readonly (readonly [string, AuditKind])[] = [
  ['staff_', 'staff'],
  ['shard_', 'fleet'],
  ['lane_', 'fleet'],
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

/** Whether `entry` belongs under `kind`. 'all' matches everything. */
export function inKind(entry: AuditEntry, kind: AuditKind): boolean {
  return kind === 'all' || auditKind(entry.action) === kind;
}
