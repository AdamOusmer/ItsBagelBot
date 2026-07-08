import type { Actions, PageServerLoad } from './$types';
import { fail } from '@sveltejs/kit';
import {
  userOverview,
  USER_MAX_PAGES,
  USER_PAGE_SIZE,
  userLookup,
  userSetStatus,
  userSetActive,
  userSetCreatorCode,
  userBan,
  userUnban,
  userReset,
  userDelete,
  tokenClear,
  tokenStatus,
  restartUserEventSub,
  channelSubState,
  auditAppend,
  type AdminUserWire,
  type ChannelSubState
} from '$lib/server/services';
import { requireAdmin, isDemo, type AdminIdentity } from '$lib/server/access';
import { signViewAs } from '@bagel/shared/server/impersonation';
import { env } from '$env/dynamic/private';
import { sampleStats, sampleUsers } from '$lib/server/sample';

const MAX_SEARCH_LENGTH = 200;
const CREATOR_CODE_MAX_LENGTH = 64;

function parsePage(raw: string | null): number {
  const page = Number(raw ?? '1');
  if (!Number.isFinite(page)) return 1;
  return Math.min(Math.max(Math.trunc(page), 1), USER_MAX_PAGES);
}

function normalizeSearch(raw: string | null): string {
  return (raw ?? '').trim().slice(0, MAX_SEARCH_LENGTH);
}

function matchesSearch(user: AdminUserWire, search: string): boolean {
  if (!search) return true;
  const q = search.toLowerCase();
  return user.username.toLowerCase().includes(q) || String(user.id).includes(q);
}

function demoPage(page: number, search: string) {
  const filtered = sampleUsers.filter((user) => matchesSearch(user, search));
  const start = (page - 1) * USER_PAGE_SIZE;
  const users = filtered.slice(start, start + USER_PAGE_SIZE);
  const cappedTotal = Math.min(filtered.length, USER_PAGE_SIZE * USER_MAX_PAGES);
  return {
    recent: users,
    stats: sampleStats,
    page,
    pageSize: USER_PAGE_SIZE,
    maxPages: USER_MAX_PAGES,
    hasMore: start + USER_PAGE_SIZE < cappedTotal,
    search,
    degraded: false
  };
}

export const load: PageServerLoad = async ({ url }) => {
  const page = parsePage(url.searchParams.get('page'));
  const search = normalizeSearch(url.searchParams.get('q'));

  if (isDemo()) {
    return demoPage(page, search);
  }
  try {
    const overview = await userOverview(page, search);
    return {
      recent: overview.users,
      stats: overview.stats,
      page: overview.page,
      pageSize: overview.page_size,
      maxPages: overview.max_pages,
      hasMore: overview.has_more,
      search,
      degraded: false
    };
  } catch {
    return {
      recent: [],
      stats: sampleStats,
      page,
      pageSize: USER_PAGE_SIZE,
      maxPages: USER_MAX_PAGES,
      hasMore: false,
      search,
      degraded: true
    };
  }
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
      return { lookup: { user: u, tokenPresent: u.status !== 'free', subState: { state: 'ok', error: '', checkedAt: null } as ChannelSubState } };
    }
    try {
      const user = await userLookup(q);
      const uid = String(user.id);
      const [tokenRes, subRes] = await Promise.allSettled([tokenStatus(uid), channelSubState(uid)]);
      const present = tokenRes.status === 'fulfilled' ? tokenRes.value.present : false;
      const subState: ChannelSubState = subRes.status === 'fulfilled' ? subRes.value : { state: 'unknown', error: '', checkedAt: null };
      return { lookup: { user, tokenPresent: present, subState } };
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

    // A paid grant always carries its end date (the users service enforces it
    // too). The grant runs from today until end-of-day on the chosen date.
    let expiresAt: string | undefined;
    let detail = `status=${status}`;
    if (status === 'paid') {
      const raw = String(f.get('expires_at') ?? '').trim(); // YYYY-MM-DD from the modal
      if (!/^\d{4}-\d{2}-\d{2}$/.test(raw)) {
        return { action: { ok: false, notice: 'paid grant needs an end date' } };
      }
      const end = new Date(`${raw}T23:59:59.999Z`);
      if (Number.isNaN(end.getTime()) || end.getTime() <= Date.now()) {
        return { action: { ok: false, notice: 'end date must be in the future' } };
      }
      if (end.getTime() > Date.now() + 5 * 365 * 864e5) {
        return { action: { ok: false, notice: 'end date is too far out (max 5 years)' } };
      }
      expiresAt = end.toISOString();
      const start = new Date().toISOString().slice(0, 10);
      detail = `status=paid start=${start} end=${raw}`;
    }

    if (isDemo()) return { action: { ok: true, notice: `status set to ${status} (demo)` } };
    try {
      const user: AdminUserWire = await userSetStatus(userId, status, expiresAt);
      audit(admin, 'set_status', userId, detail, true);
      const until = user.subscription_expires_at
        ? ` until ${user.subscription_expires_at.slice(0, 10)}`
        : '';
      return { action: { ok: true, notice: `status set to ${user.status}${until}` }, lookup: { user } };
    } catch (e) {
      audit(admin, 'set_status', userId, detail, false, (e as Error).message);
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

  setCreatorCode: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });
    const f = await request.formData();
    const userId = String(f.get('user_id') ?? '').trim();
    const creatorCode = String(f.get('creator_code') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    if (creatorCode.length > CREATOR_CODE_MAX_LENGTH) {
      return { action: { ok: false, notice: `creator code must be ${CREATOR_CODE_MAX_LENGTH} characters or fewer` } };
    }

    const detail = creatorCode ? `creator_code=${creatorCode}` : 'creator_code=cleared';
    if (isDemo()) {
      const user = sampleUsers.find((u) => String(u.id) === userId);
      return {
        action: { ok: true, notice: creatorCode ? `creator code set to ${creatorCode} (demo)` : 'creator code cleared (demo)' },
        lookup: user ? { user: { ...user, creator_code: creatorCode || null } } : undefined
      };
    }
    try {
      const user: AdminUserWire = await userSetCreatorCode(userId, creatorCode);
      audit(admin, 'set_creator_code', userId, detail, true);
      return {
        action: { ok: true, notice: user.creator_code ? `creator code set to ${user.creator_code}` : 'creator code cleared' },
        lookup: { user }
      };
    } catch (e) {
      audit(admin, 'set_creator_code', userId, detail, false, (e as Error).message);
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
    if (isDemo()) return { action: { ok: true, notice: 'bot restarted (demo only, no real subs dropped)' }, subState: { state: 'ok', error: '', checkedAt: null } as ChannelSubState };
    try {
      await restartUserEventSub(userId);
      audit(admin, 'restart', userId, '', true);
      const subState: ChannelSubState = await channelSubState(userId).catch(() => ({ state: 'unknown' as const, error: '', checkedAt: null }));
      return { action: { ok: true, notice: 'bot restarted (atomic reconnect queued)' }, subState };
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
