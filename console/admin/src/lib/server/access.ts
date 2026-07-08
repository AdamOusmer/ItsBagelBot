// Admin access control. Authorization is DB-backed: a request must carry a
// valid session whose Twitch user_id is an active row in the admin allowlist
// (served by the users service over NATS, auth.check). The tailnet is the
// network boundary; this is the identity boundary on top of it. DEMO=1
// synthesizes an allowed superadmin so the panel renders without auth wired up.
// DEMO is read from process.env, NOT $env/dynamic/private: this module is in
// the boot import graph (hooks.server.ts -> access), and even importing the
// dynamic-env proxy there deadlocks server.init (exit 13). process.env carries
// the same runtime value.
import type { Session } from './session';
import { adminCheck, type AdminRole } from './services';

export interface AdminIdentity {
  id: string;
  login: string;
  display_name: string;
  role: AdminRole;
}

export const demoSession: Session = {
  user_id: 'demo-admin',
  login: 'itsmavey',
  display_name: 'Mavey',
  role: 'streamer',
  iat: Math.floor(Date.now() / 1000),
  expires_at: Math.floor(Date.now() / 1000) + 3600
};

export function isDemo(): boolean {
  return process.env.DEMO === '1';
}

const RANK: Record<AdminRole, number> = { moderator: 1, admin: 2, owner: 3 };

// Managers (admin/owner) may view + manage the staff roster. Moderators cannot.
export function isManager(role: AdminRole): boolean {
  return role === 'admin' || role === 'owner';
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
// Caching is adminCheck's (fabric, `auth:<id>`, 5s fresh) — no separate cache
// here. The old private 30s Map gave a revoked admin up to 30s of stale access
// per replica with no invalidation path; adminCheck's key is evicted by the
// 'staff' invalidation scope, so staff changes revoke access on every replica
// within one request.
export async function requireAdmin(session: Session | null): Promise<AdminIdentity | null> {
  if (isDemo()) {
    return {
      id: demoSession.user_id,
      login: demoSession.login,
      display_name: demoSession.display_name,
      role: 'owner'
    };
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
