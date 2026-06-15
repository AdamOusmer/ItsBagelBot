import type { Actions, PageServerLoad } from './$types';
import { fail, redirect } from '@sveltejs/kit';
import { requireAdmin, isManager, canManage, isDemo, type AdminIdentity } from '$lib/server/access';
import { staffUpsert, staffRemove, adminListAccts, auditAppend, type AdminRole } from '$lib/server/rpc';
import type { AdminAcct } from '$lib/server/rpc';

const ROLES = new Set<AdminRole>(['moderator', 'admin', 'owner']);

// Sample roster so the page renders in demo without the users service.
const sampleStaff: AdminAcct[] = [
  { id: 804932984, login: 'itsmavey', display_name: 'itsmavey', role: 'owner', active: true, added_by: 0, created_at: new Date(Date.now() - 86400_000 * 30).toISOString() },
  { id: 111111111, login: 'an_admin', display_name: 'An Admin', role: 'admin', active: true, added_by: 804932984, created_at: new Date(Date.now() - 86400_000 * 7).toISOString() },
  { id: 222222222, login: 'a_mod', display_name: 'A Mod', role: 'moderator', active: true, added_by: 804932984, created_at: new Date(Date.now() - 86400_000 * 2).toISOString() }
];

export const load: PageServerLoad = async ({ locals }) => {
  const admin = await requireAdmin(locals.session);
  if (!admin) throw redirect(302, '/login');
  // Staff roster is managers-only; moderators get bounced to the overview.
  if (!isManager(admin.role)) throw redirect(302, '/');

  if (isDemo()) {
    return { staff: sampleStaff, me: admin, degraded: false };
  }
  let staff: AdminAcct[] = [];
  let degraded = false;
  try {
    staff = await adminListAccts();
  } catch {
    degraded = true;
  }
  return { staff, me: admin, degraded };
};

function audit(admin: AdminIdentity, action: string, target: string, detail: string, ok: boolean, error?: string): void {
  if (isDemo()) return;
  auditAppend({ actor_id: admin.id, actor_login: admin.login, action, target, detail, ok, error }).catch(() => {});
}

export const actions: Actions = {
  // Create or modify a staff member (add by id, or change role).
  upsert: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin || !isManager(admin.role)) return fail(403, { error: 'forbidden' });

    const f = await request.formData();
    const userId = String(f.get('user_id') ?? '').trim();
    const login = String(f.get('login') ?? '').trim();
    const displayName = String(f.get('display_name') ?? '').trim() || login;
    const role = String(f.get('role') ?? '').trim() as AdminRole;

    if (!/^[0-9]+$/.test(userId)) return fail(400, { error: 'numeric user id required' });
    if (!login) return fail(400, { error: 'login required' });
    if (!ROLES.has(role)) return fail(400, { error: 'invalid role' });
    // Client-side mirror of the server ladder: block before the round-trip.
    if (!canManage(admin.role, role)) return fail(403, { error: `cannot grant ${role}` });

    if (isDemo()) return { action: { ok: true, notice: `${login} → ${role} (demo)` } };
    try {
      await staffUpsert({ id: admin.id, role: admin.role }, { userId, login, displayName, role });
      audit(admin, 'staff_upsert', userId, `${login}:${role}`, true);
      return { action: { ok: true, notice: `${login} set to ${role}` } };
    } catch (e) {
      audit(admin, 'staff_upsert', userId, `${login}:${role}`, false, (e as Error).message);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  // Soft-remove (deactivate) a staff member.
  remove: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin || !isManager(admin.role)) return fail(403, { error: 'forbidden' });

    const f = await request.formData();
    const userId = String(f.get('user_id') ?? '').trim();
    const targetRole = String(f.get('target_role') ?? '').trim() as AdminRole;
    if (!/^[0-9]+$/.test(userId)) return fail(400, { error: 'numeric user id required' });
    if (targetRole && !canManage(admin.role, targetRole)) return fail(403, { error: 'cannot remove this member' });

    if (isDemo()) return { action: { ok: true, notice: 'staff removed (demo)' } };
    try {
      await staffRemove({ id: admin.id, role: admin.role }, userId);
      audit(admin, 'staff_remove', userId, targetRole, true);
      return { action: { ok: true, notice: 'staff member removed' } };
    } catch (e) {
      audit(admin, 'staff_remove', userId, targetRole, false, (e as Error).message);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  }
};
