// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

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
  isForbidden,
  type AdminUserWire,
  type ChannelSubState,
  type UserRef
} from '$lib/server/services';
import { requireRole, type AccessKey, type AdminIdentity } from '$lib/server/access';
import {
  audited,
  badRequest,
  okReply,
  refusalReply,
  refused,
  softNotice,
  type ParseRefusal,
  type ParseResult
} from '$lib/server/admin-action';
import { audit } from '$lib/server/audit';
import { signViewAs } from '@bagel/kit/server/impersonation';
import { env } from '$env/dynamic/private';
import { EMPTY_USER_STATS } from '$lib/server/fallback';
import type { UserStats } from '@bagel/kit';
import { parsePage, normalizeSearch } from '$lib/server/paging';

const CREATOR_CODE_MAX_LENGTH = 64;
const DEMO = dev && process.env.DEMO === '1';

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

// The directory's query string, parsed once. The three travel together through
// every layer below (demo fixtures, RPC, the returned page props), so they move
// as one value rather than as three positional strings a caller can reorder.
export type DirectoryQuery = {
  page: number;
  search: string;
  state: string;
};

type DemoDirectoryFixtures = {
  sampleUsers: AdminUserWire[];
  sampleStats: UserStats;
};

function demoPage(q: DirectoryQuery, fixtures: DemoDirectoryFixtures) {
  const filtered = fixtures.sampleUsers.filter(
    (user) => matchesSearch(user, q.search) && matchesState(user, q.state)
  );
  const start = (q.page - 1) * USER_PAGE_SIZE;
  const users = filtered.slice(start, start + USER_PAGE_SIZE);
  const cappedTotal = Math.min(filtered.length, USER_PAGE_SIZE * USER_MAX_PAGES);
  return {
    recent: users,
    stats: fixtures.sampleStats,
    page: q.page,
    pageSize: USER_PAGE_SIZE,
    maxPages: USER_MAX_PAGES,
    hasMore: start + USER_PAGE_SIZE < cappedTotal,
    search: q.search,
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

async function loadDirectory(actorId: string, q: DirectoryQuery): Promise<UserDirectory> {
  try {
    const overview = await userOverview(actorId, q.page, q.search, q.state);
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
      page: q.page,
      pageSize: USER_PAGE_SIZE,
      maxPages: USER_MAX_PAGES,
      hasMore: false,
      degraded: true
    };
  }
}

// Streamed: the shell (toolbar, headers) renders immediately; the directory
// hydrates when the users RPC lands instead of blocking SSR on NATS.
export const load: PageServerLoad = async ({ url, parent }) => {
  const { id } = await parent();
  const q: DirectoryQuery = {
    page: parsePage(url.searchParams.get('page'), USER_MAX_PAGES),
    search: normalizeSearch(url.searchParams.get('q')),
    state: parseState(url.searchParams.get('state'))
  };

  const directory: Promise<UserDirectory> = DEMO
    ? import('$lib/server/demo-data').then((fixtures) => demoPage(q, fixtures))
    : loadDirectory(id, q);

  return { directory, ...q };
};

// Status values the users service accepts (raw DB enum).
const STATUSES = new Set(['free', 'paid', 'vip']);

function dashboardOrigin(url: URL): string {
  const configured = (env.DASHBOARD_PUBLIC_ORIGIN ?? '').trim().replace(/\/+$/, '');
  if (configured) return configured;
  if (dev) return url.origin;
  throw new Error('DASHBOARD_PUBLIC_ORIGIN not set');
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

// Who is searching and what they typed. Same reason as services.ts's UserRef:
// both are strings, so the two orders are indistinguishable to the compiler.
type LookupRef = {
  actorId: string;
  q: string;
};

// probeUser fetches the row plus its token/enroll state; allSettled keeps a
// slow or down responder from failing the whole lookup.
async function probeUser(ref: LookupRef) {
  const user = await userLookup(ref.actorId, ref.q);
  const uid = String(user.id);
  const [tokenRes, subRes] = await Promise.allSettled([
    tokenStatus({ actorId: ref.actorId, userId: uid }),
    channelSubState(uid)
  ]);
  return {
    user,
    tokenPresent: tokenRes.status === 'fulfilled' ? tokenRes.value.present : false,
    subState: subRes.status === 'fulfilled' ? subRes.value : unknownSubState
  };
}

// ── The shared per-user action shape ────────────────────────────────────────
//
// Every per-user action runs the same six steps: gate on the role, read the
// target id, parse the verb's own form fields, short-circuit under DEMO, run
// the mutation, audit the outcome. Only the parse and the mutation differ, so
// a spec supplies those and userAction owns the rest.
//
// setStatus and setCreatorCode used to be written out by hand, because the
// wrapper had no way to express "this verb has extra fields to validate" or
// "this verb phrases its own success notice". `parse` and `notice` are that
// way. Keeping them hand-written meant the gate, the audit line and the
// refusal mapping existed in three copies that had already drifted apart.

// Verbs whose only input is the target id parse to no payload at all.
const noFields = () => ({ value: null });

// Everything a gated, parsed action has to work with.
type ActionCtx<P> = {
  admin: AdminIdentity;
  ref: UserRef;
  payload: P;
};

type UserActionSpec<P> = {
  name: string; // audit action id
  key: AccessKey; // least role that may run it (ROLE_FOR)
  parse: (f: FormData) => ParseResult<P>;
  demo: (ctx: ActionCtx<P>) => unknown;
  notice: (user: AdminUserWire | null, payload: P) => string;
  detail?: (payload: P) => string;
  // Returns the refreshed user row when the service echoes one (so the
  // inspector panel updates), or null for row-less mutations.
  run: (ref: UserRef, payload: P) => Promise<AdminUserWire | null>;
};

function runUserAction<P>(ctx: ActionCtx<P>, spec: UserActionSpec<P>) {
  return audited(
    {
      admin: ctx.admin,
      action: spec.name,
      target: ctx.ref.userId,
      detail: spec.detail?.(ctx.payload)
    },
    () => spec.run(ctx.ref, ctx.payload),
    // A row-less mutation (clear token, delete) has nothing to refresh the
    // inspector with, so the lookup half is omitted rather than sent as null.
    (user) => {
      const reply = okReply(spec.notice(user, ctx.payload));
      return user ? { ...reply, lookup: { user } } : reply;
    }
  );
}

function userAction<P>(spec: UserActionSpec<P>) {
  return async ({ request, locals }: { request: Request; locals: App.Locals }) => {
    const admin = await requireRole({ locals }, spec.key);
    if (!admin) return fail(403, { error: 'forbidden' });
    const f = await request.formData();
    const userId = String(f.get('user_id') ?? '').trim();
    if (!userId) return fail(400, { error: 'user_id required' });

    const parsed = spec.parse(f);
    if ('refuse' in parsed) return refusalReply(parsed);

    const ctx: ActionCtx<P> = { admin, ref: { actorId: admin.id, userId }, payload: parsed.value };
    if (DEMO) return spec.demo(ctx);
    return runUserAction(ctx, spec);
  };
}

// ── Per-verb parse and notice steps ─────────────────────────────────────────

// parsePaidGrant validates the status modal's end date: a paid grant always
// carries one (the users service enforces it too) and runs from today until
// end-of-day on the chosen date.
function parsePaidGrant(f: FormData): { expiresAt: string; detail: string } | ParseRefusal {
  const raw = String(f.get('expires_at') ?? '').trim(); // YYYY-MM-DD from the modal
  if (!/^\d{4}-\d{2}-\d{2}$/.test(raw)) {
    return softNotice('paid grant needs an end date');
  }
  const end = new Date(`${raw}T23:59:59.999Z`);
  if (Number.isNaN(end.getTime()) || end.getTime() <= Date.now()) {
    return softNotice('end date must be in the future');
  }
  if (end.getTime() > Date.now() + 5 * 365 * 864e5) {
    return softNotice('end date is too far out (max 5 years)');
  }
  const start = new Date().toISOString().slice(0, 10);
  return { expiresAt: end.toISOString(), detail: `status=paid start=${start} end=${raw}` };
}

type StatusGrant = { status: string; expiresAt?: string; detail: string };

function parseStatus(f: FormData): ParseResult<StatusGrant> {
  const status = String(f.get('status') ?? '').trim();
  if (!STATUSES.has(status)) return badRequest('invalid status');
  if (status !== 'paid') return { value: { status, detail: `status=${status}` } };
  const grant = parsePaidGrant(f);
  if ('refuse' in grant) return grant;
  return { value: { status, expiresAt: grant.expiresAt, detail: grant.detail } };
}

function statusNotice(user: AdminUserWire | null, grant: StatusGrant): string {
  if (!user) return `status set to ${grant.status}`;
  const until = user.subscription_expires_at
    ? ` until ${user.subscription_expires_at.slice(0, 10)}`
    : '';
  return `status set to ${user.status}${until}`;
}

type CreatorCode = { code: string; detail: string };

function parseCreatorCode(f: FormData): ParseResult<CreatorCode> {
  const code = String(f.get('creator_code') ?? '').trim();
  if (code.length > CREATOR_CODE_MAX_LENGTH) {
    return softNotice(`creator code must be ${CREATOR_CODE_MAX_LENGTH} characters or fewer`);
  }
  return { value: { code, detail: code ? `creator_code=${code}` : 'creator_code=cleared' } };
}

function creatorCodeNotice(user: AdminUserWire | null): string {
  return user?.creator_code ? `creator code set to ${user.creator_code}` : 'creator code cleared';
}

function creatorCodeDemoNotice(ctx: ActionCtx<CreatorCode>) {
  const { code } = ctx.payload;
  return okReply(code ? `creator code set to ${code} (demo)` : 'creator code cleared (demo)');
}

// The demo directory is a static fixture list, so the refreshed row the
// inspector expects has to be assembled here; no service echoes one back.
async function demoCreatorCode(ctx: ActionCtx<CreatorCode>) {
  const { sampleUsers } = await import('$lib/server/demo-data');
  const user = sampleUsers.find((u) => String(u.id) === ctx.ref.userId);
  const { code } = ctx.payload;
  return {
    ...creatorCodeDemoNotice(ctx),
    lookup: user ? { user: { ...user, creator_code: code || null } } : undefined
  };
}

function parseActive(f: FormData): ParseResult<{ active: boolean }> {
  return { value: { active: String(f.get('active') ?? '').trim() === 'true' } };
}

// Serving state and Twitch EventSub enrollment move together, mirroring the
// dashboard's connect/disconnect. Every verb that stops serving a user
// (deactivate, ban, delete) unenrolls before its mutation, so no failure mode
// leaves a non-served user with live subscriptions; verbs that may resume
// serving enroll afterwards, and only when the refreshed row is actually
// served again (active and not banned).
type EnrollmentSyncedMutation<T> = {
  userId: string;
  sync: 'unenroll-first' | 'enroll-after';
  mutate: () => Promise<T>;
};

function served(user: AdminUserWire | null): boolean {
  return user !== null && user.is_active && !user.banned;
}

async function withEnrollmentSync<T extends AdminUserWire | null>(
  m: EnrollmentSyncedMutation<T>
): Promise<T> {
  if (m.sync === 'unenroll-first') {
    await publishUserEventSub(m.userId, false);
    return m.mutate();
  }
  const user = await m.mutate();
  if (served(user)) await publishUserEventSub(m.userId, true);
  return user;
}

export const actions: Actions = {
  lookup: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'users.read');
    if (!admin) return fail(403, { error: 'forbidden' });
    const q = String((await request.formData()).get('q') ?? '').trim();
    if (!q) return fail(400, { error: 'query required' });
    if (q.length > 128) return fail(400, { error: 'query too long' });
    if (DEMO) {
      const { sampleUsers } = await import('$lib/server/demo-data');
      return demoLookup(q, sampleUsers);
    }
    try {
      return { lookup: await probeUser({ actorId: admin.id, q }) };
    } catch (e) {
      if (isForbidden(e)) return refused(e);
      return { lookup: { error: (e as Error).message, q } };
    }
  },

  setStatus: userAction<StatusGrant>({
    name: 'set_status',
    key: 'users.grant',
    parse: parseStatus,
    demo: (ctx) => okReply(`status set to ${ctx.payload.status} (demo)`),
    detail: (p) => p.detail,
    notice: statusNotice,
    run: (ref, p) => userSetStatus(ref, p.status, p.expiresAt)
  }),

  reset: userAction({
    name: 'reset',
    key: 'users.grant',
    parse: noFields,
    demo: () => okReply('user reset (demo)'),
    notice: () => 'user reset',
    run: (ref) => userReset(ref)
  }),

  clearToken: userAction({
    name: 'clear_token',
    key: 'users.token',
    parse: noFields,
    demo: () => okReply('token cleared (demo)'),
    notice: () => 'token cleared',
    run: async (ref) => {
      await tokenClear(ref);
      return null;
    }
  }),

  setActive: userAction({
    name: 'set_active',
    key: 'users.grant',
    parse: parseActive,
    demo: () => okReply('active set (demo)'),
    notice: (user) => `active=${user?.is_active}`,
    detail: (p) => String(p.active),
    run: (ref, p) =>
      withEnrollmentSync({
        userId: ref.userId,
        sync: p.active ? 'enroll-after' : 'unenroll-first',
        mutate: () => userSetActive(ref, p.active)
      })
  }),

  setCreatorCode: userAction<CreatorCode>({
    name: 'set_creator_code',
    key: 'users.grant',
    parse: parseCreatorCode,
    // The fixture import must be named behind the build-time DEMO constant,
    // not merely called behind it: a plain `demo: demoCreatorCode` leaves the
    // spec object referencing the function in a production build, nothing can
    // shake it out, and the demo-data chunk ships. `if (DEMO)` inside the
    // function is too late for the same reason. scripts/assert-production-clean.ts
    // is the gate that catches this, and it caught exactly this.
    demo: DEMO ? demoCreatorCode : creatorCodeDemoNotice,
    detail: (p) => p.detail,
    notice: creatorCodeNotice,
    run: (ref, p) => userSetCreatorCode(ref, p.code)
  }),

  ban: userAction({
    name: 'ban',
    key: 'users.ban',
    parse: noFields,
    demo: () => okReply('user banned (demo)'),
    notice: () => 'user banned',
    run: (ref) =>
      withEnrollmentSync({
        userId: ref.userId,
        sync: 'unenroll-first',
        mutate: () => userBan(ref)
      })
  }),

  unban: userAction({
    name: 'unban',
    key: 'users.ban',
    parse: noFields,
    demo: () => okReply('user unbanned (demo)'),
    notice: () => 'user unbanned',
    run: (ref) =>
      withEnrollmentSync({
        userId: ref.userId,
        sync: 'enroll-after',
        mutate: () => userUnban(ref)
      })
  }),

  restart: async ({ request, locals }) => {
    const admin = await requireRole({ locals }, 'users.restart');
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
    const admin = await requireRole({ locals }, 'users.impersonate');
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
      const user = await userLookup(admin.id, userId);
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
      if (isForbidden(e)) return refused(e);
      return { action: { ok: false, notice: (e as Error).message } };
    }
  },

  delete: userAction({
    name: 'delete',
    key: 'users.delete',
    parse: noFields,
    demo: () => okReply('user deleted (demo only, no real data removed)'),
    notice: () => 'user deleted',
    run: (ref) =>
      withEnrollmentSync({
        userId: ref.userId,
        sync: 'unenroll-first',
        mutate: async () => {
          await userDelete(ref);
          return null;
        }
      })
  })
};
