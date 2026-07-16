import type { Actions, PageServerLoad } from './$types';
import { fail } from '@sveltejs/kit';
import { dev } from '$app/environment';
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
  publishUserEventSub,
  channelSubState,
  auditAppend,
  type AdminUserWire,
  type ChannelSubState
} from '$lib/server/services';
import { requireAdmin, type AdminIdentity } from '$lib/server/access';
import { signViewAs } from '@bagel/shared/server/impersonation';
import { env } from '$env/dynamic/private';
import { EMPTY_USER_STATS } from '$lib/server/fallback';
import type { UserStats } from '@bagel/shared';

const MAX_SEARCH_LENGTH = 200;
const CREATOR_CODE_MAX_LENGTH = 64;
const DEMO = dev && process.env.DEMO === '1';

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

// Effective-state filter values the users service understands. Precedence
// there mirrors the row color: banned beats inactive beats tier.
const STATES = new Set(['vip', 'paid', 'free', 'banned', 'inactive']);

function parseState(raw: string | null): string {
  const state = (raw ?? '').trim();
  return STATES.has(state) ? state : '';
}

function matchesState(user: AdminUserWire, state: string): boolean {
  if (!state) return true;
  if (user.banned) return state === 'banned';
  if (!user.is_active) return state === 'inactive';
  return user.status === state;
}

type DemoDirectoryFixtures = {
  sampleUsers: AdminUserWire[];
  sampleStats: UserStats;
};

function demoPage(page: number, search: string, state: string, fixtures: DemoDirectoryFixtures) {
  const filtered = fixtures.sampleUsers.filter(
    (user) => matchesSearch(user, search) && matchesState(user, state)
  );
  const start = (page - 1) * USER_PAGE_SIZE;
  const users = filtered.slice(start, start + USER_PAGE_SIZE);
  const cappedTotal = Math.min(filtered.length, USER_PAGE_SIZE * USER_MAX_PAGES);
  return {
    recent: users,
    stats: fixtures.sampleStats,
    page,
    pageSize: USER_PAGE_SIZE,
    maxPages: USER_MAX_PAGES,
    hasMore: start + USER_PAGE_SIZE < cappedTotal,
    search,
    degraded: false
  };
}

export type UserDirectory = {
  recent: AdminUserWire[];
  stats: UserStats;
  page: number;
  pageSize: number;
  maxPages: number;
  hasMore: boolean;
  degraded: boolean;
};

async function loadDirectory(page: number, search: string, state: string): Promise<UserDirectory> {
  try {
    const overview = await userOverview(page, search, state);
    return {
      recent: overview.users,
      stats: overview.stats,
      page: overview.page,
      pageSize: overview.page_size,
      maxPages: overview.max_pages,
      hasMore: overview.has_more,
      degraded: false
    };
  } catch {
    return {
      recent: [],
      stats: { ...EMPTY_USER_STATS },
      page,
      pageSize: USER_PAGE_SIZE,
      maxPages: USER_MAX_PAGES,
      hasMore: false,
      degraded: true
    };
  }
}

// Streamed: the shell (toolbar, headers) renders immediately; the directory
// hydrates when the users RPC lands instead of blocking SSR on NATS.
export const load: PageServerLoad = ({ url }) => {
  const page = parsePage(url.searchParams.get('page'));
  const search = normalizeSearch(url.searchParams.get('q'));
  const state = parseState(url.searchParams.get('state'));

  const directory: Promise<UserDirectory> = DEMO
    ? import('$lib/server/demo-data').then((fixtures) => demoPage(page, search, state, fixtures))
    : loadDirectory(page, search, state);

  return { directory, page, search, state };
};

// Status values the users service accepts (raw DB enum).
const STATUSES = new Set(['free', 'paid', 'vip']);

function dashboardOrigin(url: URL): string {
  const configured = (env.DASHBOARD_PUBLIC_ORIGIN ?? '').trim().replace(/\/+$/, '');
  if (configured) return configured;
  if (dev) return url.origin;
  throw new Error('DASHBOARD_PUBLIC_ORIGIN not set');
}

type AuditOutcome = {
  action: string;
  target: string;
  detail?: string;
  ok: boolean;
  error?: string;
};

// audit records a mutating action best-effort: a logging failure must never
// block or fail the operator action it describes. Skipped in demo (synthetic
// non-numeric actor id).
function audit(admin: AdminIdentity, outcome: AuditOutcome): void {
  if (DEMO) return;
  auditAppend({
    actor_id: admin.id,
    actor_login: admin.login,
    action: outcome.action,
    target: outcome.target,
    detail: outcome.detail ?? '',
    ok: outcome.ok,
    error: outcome.error
  }).catch(() => {});
}

const unknownSubState: ChannelSubState = { state: 'unknown', error: '', checkedAt: null };

function demoLookup(q: string, sampleUsers: AdminUserWire[]) {
  const u = sampleUsers.find((s) => s.username === q || String(s.id) === q);
  if (!u) return { lookup: { error: 'user not found', q } };
  return {
    lookup: {
      user: u,
      tokenPresent: u.status !== 'free',
      subState: { state: 'ok', error: '', checkedAt: null } as ChannelSubState
    }
  };
}

// probeUser fetches the row plus its token/enroll state; allSettled keeps a
// slow or down responder from failing the whole lookup.
async function probeUser(q: string) {
  const user = await userLookup(q);
  const uid = String(user.id);
  const [tokenRes, subRes] = await Promise.allSettled([tokenStatus(uid), channelSubState(uid)]);
  return {
    user,
    tokenPresent: tokenRes.status === 'fulfilled' ? tokenRes.value.present : false,
    subState: subRes.status === 'fulfilled' ? subRes.value : unknownSubState
  };
}

// parsePaidGrant validates the status modal's end date: a paid grant always
// carries one (the users service enforces it too) and runs from today until
// end-of-day on the chosen date.
function parsePaidGrant(f: FormData): { expiresAt: string; detail: string } | { notice: string } {
  const raw = String(f.get('expires_at') ?? '').trim(); // YYYY-MM-DD from the modal
  if (!/^\d{4}-\d{2}-\d{2}$/.test(raw)) {
    return { notice: 'paid grant needs an end date' };
  }
  const end = new Date(`${raw}T23:59:59.999Z`);
  if (Number.isNaN(end.getTime()) || end.getTime() <= Date.now()) {
    return { notice: 'end date must be in the future' };
  }
  if (end.getTime() > Date.now() + 5 * 365 * 864e5) {
    return { notice: 'end date is too far out (max 5 years)' };
  }
  const start = new Date().toISOString().slice(0, 10);
  return { expiresAt: end.toISOString(), detail: `status=paid start=${start} end=${raw}` };
}

// userAction wraps the shared per-user mutation shape: admin gate, user_id
// parse, demo short-circuit, the RPC, the audit trail, and the notice reply.
// run returns the refreshed user row when the service echoes one (so the
// inspector panel updates), or null for row-less mutations.
type UserActionSpec = {
  name: string; // audit action id
  demoNotice: string;
  notice: (user: AdminUserWire | null) => string;
  detail?: (f: FormData) => string;
  run: (userId: string, f: FormData) => Promise<AdminUserWire | null>;
};

function userAction(spec: UserActionSpec) {
  return async ({ request, locals }: { request: Request; locals: App.Locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });
    const f = await request.formData();
    const userId = String(f.get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    if (DEMO) return { action: { ok: true, notice: spec.demoNotice } };

    const detail = spec.detail?.(f) ?? '';
    try {
      const user = await spec.run(userId, f);
      audit(admin, { action: spec.name, target: userId, detail, ok: true });
      const reply = { action: { ok: true, notice: spec.notice(user) } };
      return user ? { ...reply, lookup: { user } } : reply;
    } catch (e) {
      audit(admin, { action: spec.name, target: userId, detail, ok: false, error: (e as Error).message });
      return { action: { ok: false, notice: (e as Error).message } };
    }
  };
}

function formActive(f: FormData): boolean {
  return String(f.get('active') ?? '').trim() === 'true';
}

export const actions: Actions = {
  lookup: async ({ request, locals }) => {
    if (!(await requireAdmin(locals.session))) return fail(403, { error: 'forbidden' });
    const q = String((await request.formData()).get('q') ?? '').trim();
    if (!q) return fail(400, { error: 'query required' });
    if (q.length > 128) return fail(400, { error: 'query too long' });
    if (DEMO) {
      const { sampleUsers } = await import('$lib/server/demo-data');
      return demoLookup(q, sampleUsers);
    }
    try {
      return { lookup: await probeUser(q) };
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

    let expiresAt: string | undefined;
    let detail = `status=${status}`;
    if (status === 'paid') {
      const grant = parsePaidGrant(f);
      if ('notice' in grant) return { action: { ok: false, notice: grant.notice } };
      ({ expiresAt, detail } = grant);
    }

    if (DEMO) return { action: { ok: true, notice: `status set to ${status} (demo)` } };
    try {
      const user: AdminUserWire = await userSetStatus(userId, status, expiresAt);
      audit(admin, { action: 'set_status', target: userId, detail, ok: true });
      const until = user.subscription_expires_at
        ? ` until ${user.subscription_expires_at.slice(0, 10)}`
        : '';
      return { action: { ok: true, notice: `status set to ${user.status}${until}` }, lookup: { user } };
    } catch (e) {
      audit(admin, { action: 'set_status', target: userId, detail, ok: false, error: (e as Error).message });
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  reset: userAction({
    name: 'reset',
    demoNotice: DEMO ? 'user reset (demo)' : '',
    notice: () => 'user reset',
    run: (userId) => userReset(userId)
  }),

  clearToken: userAction({
    name: 'clear_token',
    demoNotice: DEMO ? 'token cleared (demo)' : '',
    notice: () => 'token cleared',
    run: async (userId) => {
      await tokenClear(userId);
      return null;
    }
  }),

  setActive: userAction({
    name: 'set_active',
    demoNotice: DEMO ? 'active set (demo)' : '',
    notice: (user) => `active=${user?.is_active}`,
    detail: (f) => String(formActive(f)),
    // The flag and the Twitch EventSub enrollment move together, mirroring the
    // dashboard's connect/disconnect. Ordered so no failure mode leaves an
    // inactive user with live subscriptions: activating flips the flag before
    // enrolling, deactivating unenrolls before flipping the flag.
    run: async (userId, f) => {
      if (formActive(f)) {
        const user = await userSetActive(userId, true);
        await publishUserEventSub(userId, true);
        return user;
      }
      await publishUserEventSub(userId, false);
      return userSetActive(userId, false);
    }
  }),

  // Creator code carries its own length validation and demo-lookup shaping, so
  // it stays hand-written rather than going through userAction.
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
    if (DEMO) {
      const { sampleUsers } = await import('$lib/server/demo-data');
      const user = sampleUsers.find((u) => String(u.id) === userId);
      return {
        action: { ok: true, notice: creatorCode ? `creator code set to ${creatorCode} (demo)` : 'creator code cleared (demo)' },
        lookup: user ? { user: { ...user, creator_code: creatorCode || null } } : undefined
      };
    }
    try {
      const user: AdminUserWire = await userSetCreatorCode(userId, creatorCode);
      audit(admin, { action: 'set_creator_code', target: userId, detail, ok: true });
      return {
        action: { ok: true, notice: user.creator_code ? `creator code set to ${user.creator_code}` : 'creator code cleared' },
        lookup: { user }
      };
    } catch (e) {
      audit(admin, { action: 'set_creator_code', target: userId, detail, ok: false, error: (e as Error).message });
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  ban: userAction({
    name: 'ban',
    demoNotice: DEMO ? 'user banned (demo)' : '',
    notice: () => 'user banned',
    run: (userId) => userBan(userId)
  }),

  unban: userAction({
    name: 'unban',
    demoNotice: DEMO ? 'user unbanned (demo)' : '',
    notice: () => 'user unbanned',
    run: (userId) => userUnban(userId)
  }),

  restart: async ({ request, locals }) => {
    const admin = await requireAdmin(locals.session);
    if (!admin) return fail(403, { error: 'forbidden' });
    const userId = String((await request.formData()).get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });
    if (DEMO) {
      return {
        action: { ok: true, notice: 'bot restarted (demo only, no real subs dropped)' },
        subState: { state: 'ok', error: '', checkedAt: null } as ChannelSubState
      };
    }
    try {
      await restartUserEventSub(userId);
      audit(admin, { action: 'restart', target: userId, ok: true });
      const subState: ChannelSubState = await channelSubState(userId).catch(() => unknownSubState);
      return { action: { ok: true, notice: 'bot restarted (atomic reconnect queued)' }, subState };
    } catch (e) {
      audit(admin, { action: 'restart', target: userId, ok: false, error: (e as Error).message });
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
    if (DEMO) {
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
      audit(admin, { action: 'impersonate', target: userId, ok: true });
      return {
        action: { ok: true, notice: 'view-as link minted (valid 5 min)' },
        viewAsUrl: `${origin}/auth/impersonate?t=${token}`
      };
    } catch (e) {
      audit(admin, { action: 'impersonate', target: userId, ok: false, error: (e as Error).message });
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  delete: userAction({
    name: 'delete',
    demoNotice: DEMO ? 'user deleted (demo only, no real data removed)' : '',
    notice: () => 'user deleted',
    run: async (userId) => {
      await userDelete(userId);
      return null;
    }
  })
};
