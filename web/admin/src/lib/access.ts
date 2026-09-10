// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// The role ladder itself, with no server dependency, so a page component can
// ask the same question its server action will ask.
//
// It used to live entirely in $lib/server/access.ts. Splitting the pure half
// out is not a second table: server/access.ts re-exports these symbols, so
// there is still exactly one ROLE_FOR in the app. What the split buys is the
// CLIENT half of "defense in depth": the users inspector must decide whether to
// OFFER a ban button, and importing $lib/server from a component is illegal in
// SvelteKit (it would drag the NATS client into the browser bundle). Before
// this, that decision was a hand-written `role === 'admin' || role === 'owner'`
// beside each button, i.e. exactly the scattered check ROLE_FOR exists to kill.
//
// Visibility is never the enforcement. Every key below is also checked by
// requireRole on the server, and most by the backing service on top of that; a
// hidden button is a courtesy, not a boundary.
import { STAFF_RANK as RANK, type StaffRole } from '@bagel/shared/staff-role';

export type AdminRole = StaffRole;

// Managers (admin/owner) may view + manage the staff roster. Moderators cannot.
export function isManager(role: AdminRole): boolean {
  return role === 'admin' || role === 'owner';
}

// ROLE_FOR is this console's whole authorization policy: one row per operator
// action, naming the least role that may perform it. A table rather than an
// `isManager(...)` call scattered through nine route files, because scattered
// checks are how three mutations (shard scale, lane delete, impersonate) ended
// up gated only by "is staff at all".
//
// Where the backing service enforces the same ladder (every users.* row below
// maps onto admin.go's minRole table), this is defense in depth and the answer
// still comes from the service. Where it does not -- the ingress, the JetStream
// lane store, notifications, loyalty counters -- this table IS the enforcement,
// which is why those rows are not optional.
export const ROLE_FOR = {
  // users service (mirrors app/db/users/rpc/admin.go)
  'users.read': 'moderator',
  'users.ban': 'moderator',
  'users.grant': 'admin',
  'users.token': 'admin',
  'users.delete': 'owner',
  // console-only surfaces
  'users.impersonate': 'admin',
  'users.restart': 'admin',
  'shards.scale': 'admin',
  'lanes.mutate': 'admin',
  'notifications.send': 'admin',
  'counters.manage': 'owner',
  'staff.manage': 'admin',
  'audit.read': 'admin',
  'secrets.manage': 'owner',
  // The bot-account OAuth consent flow installs a live Twitch token for the
  // account the bot speaks as. Owner-only, and no lower: it is the one flow
  // that mints credentials from an unauthenticated-looking URL.
  'bot.token': 'owner'
} as const satisfies Record<string, AdminRole>;

export type AccessKey = keyof typeof ROLE_FOR;

// allows answers the ladder question for a role already in hand (a layout load
// that resolved the identity through parent(), or the `role` a page received
// from it), so a page gate and an action gate cannot disagree about what a key
// means.
export function allows(role: AdminRole, key: AccessKey): boolean {
  return RANK[role] >= RANK[ROLE_FOR[key]];
}

// canManage decides whether an actor may modify/remove a target staff row.
// Owners may manage anyone; admins may manage moderators and admins but never
// an owner. Mirrors the users-service enforcement (defense in depth).
export function canManage(actor: AdminRole, target: AdminRole): boolean {
  if (!isManager(actor)) return false;
  if (target === 'owner') return actor === 'owner';
  return RANK[actor] >= RANK[target];
}

// The roles `actor` may grant, in ladder order. Derived from canManage rather
// than listed, so the staff page's radio options cannot drift from the check
// the upsert action runs.
export function grantableRoles(actor: AdminRole): AdminRole[] {
  const all: AdminRole[] = ['moderator', 'admin', 'owner'];
  return all.filter((target) => canManage(actor, target));
}
