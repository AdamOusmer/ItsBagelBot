import type { Actions, PageServerLoad } from './$types';
import { fail } from '@sveltejs/kit';
import {
  userList,
  userStats,
  userLookup,
  userSetStatus,
  userSetActive,
  userReset,
  userDelete,
  tokenClear,
  tokenStatus,
  restartUserEventSub,
  type AdminUserWire
} from '$lib/server/rpc';
import { allowed, isDemo } from '$lib/server/access';
import { sampleStats, sampleUsers } from '$lib/server/sample';

export const load: PageServerLoad = async () => {
  if (isDemo()) {
    return { recent: sampleUsers, stats: sampleStats, degraded: false };
  }
  let recent = sampleUsers;
  let stats = sampleStats;
  let degraded = false;
  try {
    recent = await userList(20);
  } catch {
    degraded = true;
  }
  try {
    stats = await userStats();
  } catch {
    degraded = true;
  }
  return { recent, stats, degraded };
};

// Status values the users service accepts (raw DB enum).
const STATUSES = new Set(['free', 'paid', 'vip']);

export const actions: Actions = {
  lookup: async ({ request, locals }) => {
    if (!allowed(locals.session)) return fail(403, { error: 'forbidden' });
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
    if (!allowed(locals.session)) return fail(403, { error: 'forbidden' });
    const f = await request.formData();
    const userId = String(f.get('user_id') ?? '').trim();
    const status = String(f.get('status') ?? '').trim();
    if (!userId || !STATUSES.has(status)) return fail(400, { error: 'invalid status' });
    if (isDemo()) return { action: { ok: true, notice: `status set to ${status} (demo)` } };
    try {
      const user: AdminUserWire = await userSetStatus(userId, status);
      return { action: { ok: true, notice: `status set to ${user.status}` }, lookup: { user } };
    } catch (e) {
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  reset: async ({ request, locals }) => {
    if (!allowed(locals.session)) return fail(403, { error: 'forbidden' });
    const userId = String((await request.formData()).get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    if (isDemo()) return { action: { ok: true, notice: 'user reset (demo)' } };
    try {
      const user = await userReset(userId);
      return { action: { ok: true, notice: 'user reset' }, lookup: { user } };
    } catch (e) {
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  clearToken: async ({ request, locals }) => {
    if (!allowed(locals.session)) return fail(403, { error: 'forbidden' });
    const userId = String((await request.formData()).get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    if (isDemo()) return { action: { ok: true, notice: 'token cleared (demo)' } };
    try {
      await tokenClear(userId);
      return { action: { ok: true, notice: 'token cleared' } };
    } catch (e) {
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  setActive: async ({ request, locals }) => {
    if (!allowed(locals.session)) return fail(403, { error: 'forbidden' });
    const f = await request.formData();
    const userId = String(f.get('user_id') ?? '').trim();
    const active = String(f.get('active') ?? '').trim() === 'true';
    if (!userId) return fail(400, { error: 'user_id required' });
    if (isDemo()) return { action: { ok: true, notice: 'active set (demo)' } };
    try {
      const user: AdminUserWire = await userSetActive(userId, active);
      return { action: { ok: true, notice: `active=${user.is_active}` }, lookup: { user } };
    } catch (e) {
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  restart: async ({ request, locals }) => {
    if (!allowed(locals.session)) return fail(403, { error: 'forbidden' });
    const userId = String((await request.formData()).get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    if (isDemo()) return { action: { ok: true, notice: 'bot restarted (demo only, no real subs dropped)' } };
    try {
      await restartUserEventSub(userId);
      return { action: { ok: true, notice: 'bot restarted (subs dropped + recreated)' } };
    } catch (e) {
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  delete: async ({ request, locals }) => {
    if (!allowed(locals.session)) return fail(403, { error: 'forbidden' });
    const userId = String((await request.formData()).get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    if (isDemo()) return { action: { ok: true, notice: 'user deleted (demo only, no real data removed)' } };
    try {
      await userDelete(userId);
      return { action: { ok: true, notice: 'user deleted' } };
    } catch (e) {
      return { action: { ok: false, notice: (e as Error).message } };
    }
  }
};
