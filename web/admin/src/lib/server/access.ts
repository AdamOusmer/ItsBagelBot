// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
import type { Session } from './session';
import { adminCheck } from './services';
import { dev } from '$app/environment';
export {
  ROLE_FOR,
  allows,
  canManage,
  isManager,
  grantableRoles,
  type AccessKey,
  type AdminRole
} from '$lib/access';
import { allows, type AccessKey } from '$lib/access';
import type { AdminRole } from '$lib/access';

export interface AdminIdentity {
  id: string;
  login: string;
  display_name: string;
  role: AdminRole;
}

// Inline the build-time check: a helper call is not folded out of the production server entries.
const DEMO = dev && process.env.DEMO === '1';

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
    return null;
  }
}

export async function requireRole(
  event: { locals: { session: Session | null } },
  key: AccessKey
): Promise<AdminIdentity | null> {
  const admin = await requireAdmin(event.locals.session);
  if (!admin) return null;
  return allows(admin.role, key) ? admin : null;
}
