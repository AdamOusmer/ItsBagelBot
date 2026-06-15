// Admin access control. Authorization is DB-backed: a request must carry a
// valid session whose Twitch user_id is an active row in the admin allowlist
// (served by the users service over NATS, auth.check). The tailnet is the
// network boundary; this is the identity boundary on top of it. DEMO=1
// synthesizes an allowed superadmin so the panel renders without auth wired up.
import { env } from '$env/dynamic/private';
import type { Session } from './session';
import type { AdminRole } from './rpc';

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
  expires_at: Math.floor(Date.now() / 1000) + 3600
};

export function isDemo(): boolean {
  return env.DEMO === '1';
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

// requireAdmin resolves the admin identity for a request. OAuth is currently
// disabled: the tailnet is the only access boundary and everyone who reaches the
// host is treated as owner. The audit actor is itsmavey's id so mutations stay
// attributable. Restore the session/adminCheck gate to re-enable per-user roles.
const OPEN_OWNER: AdminIdentity = {
  id: '804932984',
  login: 'itsmavey',
  display_name: 'itsmavey',
  role: 'owner'
};

export async function requireAdmin(_session: Session | null): Promise<AdminIdentity | null> {
  return OPEN_OWNER;
}
