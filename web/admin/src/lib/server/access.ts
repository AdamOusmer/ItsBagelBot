// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Session } from './session';
import { adminCheck } from './services';
import { allows, type AccessKey, type AdminRole } from '#lib/access';

export {
  ROLE_FOR,
  allows,
  canManage,
  isManager,
  grantableRoles,
  type AccessKey,
  type AdminRole
} from '#lib/access';

export interface AdminIdentity {
  id: string;
  login: string;
  display_name: string;
  role: AdminRole;
}

const adminLookups = new WeakMap<Session, Promise<AdminIdentity | null>>();

export async function requireAdmin(session: Session | null): Promise<AdminIdentity | null> {
  if (!session) return null;

  const cached = adminLookups.get(session);
  if (cached) return cached;

  const pending = lookupAdmin(session);
  adminLookups.set(session, pending);
  return pending;
}

async function lookupAdmin(session: Session): Promise<AdminIdentity | null> {
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
