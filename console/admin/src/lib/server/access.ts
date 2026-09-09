// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Admin access control. Authorization is DB-backed: a request must carry a
// valid session whose Twitch user_id is an active row in the admin allowlist
// (served by the users service over NATS, auth.check). The tailnet is the
// network boundary; this is the identity boundary on top of it. Local Vite
// development can synthesize an owner when DEMO=1 so the panel renders without
// auth wired up; production builds compile that branch and fixture import out.
// DEMO is read from process.env, NOT $env/dynamic/private: this module is in
// the boot import graph (hooks.server.ts -> access), and even importing the
// dynamic-env proxy there deadlocks server.init (exit 13). process.env carries
// the same runtime value.
import type { Session } from './session';
import { adminCheck, type AdminRole } from './services';
import { dev } from '$app/environment';
// The ladder is shared, not local: the nav registry decides which sections to
// OFFER from the same numbers this table uses to decide who may act. When they
// were separate, the rail offered moderators three links every route bounced.
import { STAFF_RANK as RANK } from '@bagel/shared/staff-role';

export interface AdminIdentity {
  id: string;
  login: string;
  display_name: string;
  role: AdminRole;
}

// Keep the build-time constant in this module and branch on it directly. A
// helper call is not folded across SvelteKit's split server entries; this form
// removes the import edge before adapter-node assembles the final image graph.
const DEMO = dev && process.env.DEMO === '1';

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
  'secrets.manage': 'admin',
  // The bot-account OAuth consent flow installs a live Twitch token for the
  // account the bot speaks as. Owner-only, and no lower: it is the one flow
  // that mints credentials from an unauthenticated-looking URL.
  'bot.token': 'owner'
} as const satisfies Record<string, AdminRole>;

export type AccessKey = keyof typeof ROLE_FOR;

// allows answers the ladder question for a role already in hand (a layout load
// that resolved the identity through parent()), so a page gate and an action
// gate cannot disagree about what a key means.
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

// requireAdmin resolves the admin identity for a session, or null if the session
// is absent / not active staff. The session is sealed by the Twitch OAuth
// callback (tailnet-driven); auth.check confirms allowlist membership + role.
// DEMO mode returns a synthetic owner so the console runs without OAuth + NATS.
//
// Caching is adminCheck's (fabric, `auth:<id>`, 5s fresh). No separate cache
// here. The old private 30s Map gave a revoked admin up to 30s of stale access
// per replica with no invalidation path; adminCheck's key is evicted by the
// 'staff' invalidation scope, so staff changes revoke access on every replica
// within one request.
export async function requireAdmin(session: Session | null): Promise<AdminIdentity | null> {
  if (DEMO) {
    const { demoAdminIdentity } = await import('./demo-data');
    return demoAdminIdentity();
  }
  if (!session) return null;

  try {
    const r = await adminCheck(session.user_id, session.login, session.display_name);
    if (!r.admin) return null;
    return {
      id: session.user_id,
      login: r.login ?? session.login,
      display_name: r.display_name ?? session.display_name,
      role: r.role ?? 'admin'
    };
  } catch {
    // Fail closed: if the auth service is unreachable, deny rather than admit an
    // unverified session.
    return null;
  }
}

// requireRole is requireAdmin plus the ROLE_FOR ladder: it resolves the staff
// identity for the request and returns it only when that identity may perform
// `key`. Null means refuse -- callers answer with fail(403), never by falling
// through to the mutation.
export async function requireRole(
  event: { locals: { session: Session | null } },
  key: AccessKey
): Promise<AdminIdentity | null> {
  const admin = await requireAdmin(event.locals.session);
  if (!admin) return null;
  return allows(admin.role, key) ? admin : null;
}
