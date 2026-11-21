// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { dev } from '$app/env';
import { allows, requireRole, canManage } from '#lib/server/access';
import { staffUpsert, staffRemove, adminListAccts, type AdminAcct, type AdminRole } from '#lib/server/services';
import { actionError, adminText, audited } from '#lib/server/admin-action';
import { STAFF_ROSTER } from '#lib/components/staff/staff-roles';

const ROLES = new Set<AdminRole>(['moderator', 'admin', 'owner']);
const DEMO = dev && process.env.DEMO === '1';

export type RosterBundle = { staff: AdminAcct[]; degraded: boolean };

export const load: PageServerLoad = async ({ parent, depends }) => {
  const { id, role } = await parent();
  if (!allows(role, 'staff.manage')) throw redirect(302, '/');
  depends(STAFF_ROSTER);

  const staff = DEMO
    ? import('#lib/server/demo-data').then(({ demoStaff }) => demoStaff())
    : adminListAccts();
  const roster: Promise<RosterBundle> = staff
    .then((staff) => ({ staff, degraded: false }))
    .catch(() => ({ staff: [], degraded: true }));

  return { roster, me: { id, role } };
};

export const actions: Actions = {
  upsert: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'staff.manage');
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });

    const f = await request.formData();
    const userId = String(f.get('user_id') ?? '').trim();
    const login = String(f.get('login') ?? '').trim();
    const displayName = String(f.get('display_name') ?? '').trim() || login;
    const role = String(f.get('role') ?? '').trim() as AdminRole;

    if (!/^[0-9]+$/.test(userId)) return fail(400, { error: actionError(locals.locale, 'numeric user id required') });
    if (!login) return fail(400, { error: actionError(locals.locale, 'login required') });
    if (!ROLES.has(role)) return fail(400, { error: actionError(locals.locale, 'invalid role') });
    if (!canManage(admin.role, role)) return fail(403, { error: adminText(locals.locale, 'admin.action.cannotGrantRole', { role }) });

    if (DEMO) return { action: { ok: true, notice: adminText(locals.locale, 'admin.staff.grantedDemo', { login, role }) } };
    return audited(
      { admin, action: 'staff_upsert', target: userId, detail: `${login}:${role}` },
      () => staffUpsert({ id: admin.id }, { userId, login, displayName, role }),
      (staff) => ({ action: { ok: true, notice: adminText(locals.locale, 'admin.staff.granted', { login, role }) }, staff })
    );
  },

  remove: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'staff.manage');
    if (!admin) return fail(403, { error: actionError(locals.locale, 'forbidden') });

    const f = await request.formData();
    const userId = String(f.get('user_id') ?? '').trim();
    const targetRole = String(f.get('target_role') ?? '').trim() as AdminRole;
    if (!/^[0-9]+$/.test(userId)) return fail(400, { error: actionError(locals.locale, 'numeric user id required') });
    if (targetRole && !canManage(admin.role, targetRole)) return fail(403, { error: actionError(locals.locale, 'cannot remove this member') });

    if (DEMO) return { action: { ok: true, notice: adminText(locals.locale, 'admin.staff.removedDemo') } };
    return audited(
      { admin, action: 'staff_remove', target: userId, detail: targetRole },
      () => staffRemove({ id: admin.id }, userId),
      (staff) => ({ action: { ok: true, notice: adminText(locals.locale, 'admin.staff.removed') }, staff })
    );
  }
};
