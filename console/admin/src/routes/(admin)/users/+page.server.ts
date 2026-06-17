import type { Actions, PageServerLoad } from './$types';
import { fail } from '@sveltejs/kit';
import {
  userList,
  userStats,
  userLookup,
  userSetStatus,
  userSetActive,
  userBan,
  userUnban,
  userReset,
  userDelete,
  tokenClear,
  tokenStatus,
  restartUserEventSub,
  auditAppend,
  type AdminUserWire
} from '$lib/server/rpc';
import { requireAdmin, isDemo, type AdminIdentity } from '$lib/server/access';
import { signViewAs } from '$lib/server/impersonation';
import { env } from '$env/dynamic/private';
import { sampleStats, sampleUsers } from '$lib/server/sample';

export const load: PageServerLoad = async () => {
  if (isDemo()) {
    return { recent: sampleUsers, stats: sampleStats, degraded: false };
  }
  // Two independent reads: fire together so the page waits one round trip, not
  // two serial ones. allSettled keeps the page rendering if either is down.
  const [list, stats] = await Promise.allSettled([userList(20), userStats()]);
  return {
    recent: list.status === 'fulfilled' ? list.value : sampleUsers,
    stats: stats.status === 'fulfilled' ? stats.value : sampleStats,
    degraded: list.status === 'rejected' || stats.status === 'rejected'
  };
};

// Status values the users service accepts (raw DB enum).
const STATUSES = new Set(['free', 'paid', 'vip']);

function dashboardOrigin(url: URL): string {
  const configured = (env.DASHBOARD_PUBLIC_ORIGIN ?? '').trim().replace(/\/+$/, '');
  if (configured) return configured;
  if (env.DEMO === '1' || env.NODE_ENV !== 'production') return url.origin;
  throw new Error('DASHBOARD_PUBLIC_ORIGIN not set');
}

// audit records a mutating action best-effort: a logging failure must never
// block or fail the operator action it describes. Skipped in demo (synthetic
// non-numeric actor id).
function audit(
  admin: AdminIdentity,
  action: string,
  target: string,
  detail: string,
  ok: boolean,
  error?: string
): void {
  if (isDemo()) return;
  auditAppend({ actor_id: admin.id, actor_login: admin.login, action, target, detail, ok, error }).catch(
    () => {}
  );
}

export const actions: Actions = {
  lookup: async ({ request, locals }) => {
    if (!(await requireAdmin(locals.session))) return fail(403, { error: 'forbidden' });
    const q = String((await request.formData()).get('q') ?? '').trim();
    if (!q) return fail(400, { error: 'query required' });
    if (q.length > 128) return fail(400, { error: 'query too long' });
    if (isDemo()) {
      const u = sampleUsers.find((s) => s.username === q || String(s.id) === q);
      if (!u) return { lookup: { error: 'user not found', q } };
      return { lookup: { user: u, tokenPresent: u.status !== 'free' } };
    }
    try {
      const user = await userLookup(q);
      let present = false;
      try {
        present = (await tokenStatus(String(user.id))).present;
      } catch {
        /* token status optional */
      }
      return { lookup: { user, tokenPresent: present } };
    } catch (e) {
      return { lookup: { error: (e as Error).message, q } };
    }
  },

  setStatus: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });
    const f = await request.formData();
    const userId = String(f.get('user_id') ?? '').trim();
    const status = String(f.get('status') ?? '').trim();
    if (!userId || !STATUSES.has(status)) return fail(400, { error: 'invalid status' });
    if (isDemo()) return { action: { ok: true, notice: `status set to ${status} (demo)` } };
    try {
      const user: AdminUserWire = await userSetStatus(userId, status);
      audit(admin, 'set_status', userId, status, true);
      return { action: { ok: true, notice: `status set to ${user.status}` }, lookup: { user } };
    } catch (e) {
      audit(admin, 'set_status', userId, status, false, (e as Error).message);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  reset: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });
    const userId = String((await request.formData()).get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    if (isDemo()) return { action: { ok: true, notice: 'user reset (demo)' } };
    try {
      const user = await userReset(userId);
      audit(admin, 'reset', userId, '', true);
      return { action: { ok: true, notice: 'user reset' }, lookup: { user } };
    } catch (e) {
      audit(admin, 'reset', userId, '', false, (e as Error).message);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  clearToken: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });
    const userId = String((await request.formData()).get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    if (isDemo()) return { action: { ok: true, notice: 'token cleared (demo)' } };
    try {
      await tokenClear(userId);
      audit(admin, 'clear_token', userId, '', true);
      return { action: { ok: true, notice: 'token cleared' } };
    } catch (e) {
      audit(admin, 'clear_token', userId, '', false, (e as Error).message);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  setActive: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });
    const f = await request.formData();
    const userId = String(f.get('user_id') ?? '').trim();
    const active = String(f.get('active') ?? '').trim() === 'true';
    if (!userId) return fail(400, { error: 'user_id required' });
    if (isDemo()) return { action: { ok: true, notice: 'active set (demo)' } };
    try {
      const user: AdminUserWire = await userSetActive(userId, active);
      audit(admin, 'set_active', userId, String(active), true);
      return { action: { ok: true, notice: `active=${user.is_active}` }, lookup: { user } };
    } catch (e) {
      audit(admin, 'set_active', userId, String(active), false, (e as Error).message);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  ban: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });
    const userId = String((await request.formData()).get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    if (isDemo()) return { action: { ok: true, notice: 'user banned (demo)' } };
    try {
      const user: AdminUserWire = await userBan(userId);
      audit(admin, 'ban', userId, '', true);
      return { action: { ok: true, notice: 'user banned' }, lookup: { user } };
    } catch (e) {
      audit(admin, 'ban', userId, '', false, (e as Error).message);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  unban: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });
    const userId = String((await request.formData()).get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    if (isDemo()) return { action: { ok: true, notice: 'user unbanned (demo)' } };
    try {
      const user: AdminUserWire = await userUnban(userId);
      audit(admin, 'unban', userId, '', true);
      return { action: { ok: true, notice: 'user unbanned' }, lookup: { user } };
    } catch (e) {
      audit(admin, 'unban', userId, '', false, (e as Error).message);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  restart: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });
    const userId = String((await request.formData()).get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    if (isDemo()) return { action: { ok: true, notice: 'bot restarted (demo only, no real subs dropped)' } };
    try {
      await restartUserEventSub(userId);
      audit(admin, 'restart', userId, '', true);
      return { action: { ok: true, notice: 'bot restarted (subs dropped + recreated)' } };
    } catch (e) {
      audit(admin, 'restart', userId, '', false, (e as Error).message);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  // Mint a one-shot "view as" link the admin can open to load the target's
  // dashboard. The signed token (5 min TTL) carries the actor so every write
  // during the impersonated session is attributed back to this admin.
  impersonate: async ({ request, locals, url }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });
    const userId = String((await request.formData()).get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    let origin: string;
    try {
      origin = dashboardOrigin(url);
    } catch (e) {
      return { action: { ok: false, notice: (e as Error).message } };
    }
    if (isDemo()) {
      const token = signViewAs({
        sub: userId,
        login: 'demo',
        display_name: 'Demo',
        by_id: admin.id,
        by_login: admin.login
      });
      return { action: { ok: true, notice: 'view-as link minted (demo)' }, viewAsUrl: `${origin}/auth/impersonate?t=${token}` };
    }
    try {
      const user = await userLookup(userId);
      const token = signViewAs({
        sub: String(user.id),
        login: user.username,
        display_name: user.username,
        by_id: admin.id,
        by_login: admin.login
      });
      audit(admin, 'impersonate', userId, '', true);
      return {
        action: { ok: true, notice: 'view-as link minted (valid 5 min)' },
        viewAsUrl: `${origin}/auth/impersonate?t=${token}`
      };
    } catch (e) {
      audit(admin, 'impersonate', userId, '', false, (e as Error).message);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  delete: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });
    const userId = String((await request.formData()).get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    if (isDemo()) return { action: { ok: true, notice: 'user deleted (demo only, no real data removed)' } };
    try {
      await userDelete(userId);
      audit(admin, 'delete', userId, '', true);
      return { action: { ok: true, notice: 'user deleted' } };
    } catch (e) {
      audit(admin, 'delete', userId, '', false, (e as Error).message);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  }
};
