// Admin-facing RPC wrappers over the shared NATS client. Subjects come from env
// with the same defaults as the retired Go admin tier. Every wrapper degrades
// gracefully: callers catch and fall back to sample data so SSR always renders.
import { rpc, publish } from '@bagel/shared/server/nats';
import { defineRead, defineWrite } from '@bagel/shared/server/service';
import { createCacheFabric } from '@bagel/shared/server/cache-fabric';
import { POLICY, type CachePolicy } from '@bagel/shared/server/cache-keys';
import { getServerConfig } from '@bagel/shared/server/config';
import type { ScopeMap } from '@bagel/shared/server/invalidation';
import type { ShardSnapshot, UserStats } from '@bagel/shared';
import { adminL1CacheCapacity } from './config-sanity';

// Subjects come from process.env, NOT $env/dynamic/private. This module is
// imported at boot (hooks.server.ts -> startInvalidationListener), and reading
// SvelteKit's dynamic-env proxy at module-eval time during server.init()
// deadlocks the handler import (unsettled top-level await -> exit 13). In
// adapter-node process.env carries the same values.
const SUB = {
  shards: process.env.NATS_ADMIN_SUBJECT ?? 'twitch.ingress.admin.shards.get',
  scale: process.env.NATS_SHARD_SCALE_SUBJECT ?? 'twitch.ingress.admin.shards.scale',
  autoscale: process.env.NATS_SHARD_AUTOSCALE_SUBJECT ?? 'twitch.ingress.admin.shards.autoscale',
  status: process.env.NATS_STATUS_SUBJECT_PREFIX ?? 'twitch.ingress.status',
  user: process.env.NATS_ADMIN_USER_SUBJECT_PREFIX ?? 'bagel.rpc.admin.user',
  auth: process.env.NATS_ADMIN_AUTH_SUBJECT_PREFIX ?? 'bagel.rpc.admin.user.auth',
  audit: process.env.NATS_ADMIN_AUDIT_SUBJECT_PREFIX ?? 'bagel.rpc.admin.user.audit',
  outgress: process.env.NATS_OUTGRESS_SYSTEM_SUBJECT ?? 'twitch.outgress.system',
  outgressRpc: process.env.NATS_OUTGRESS_RPC_PREFIX ?? 'bagel.rpc.outgress',
  notifications: process.env.NATS_ADMIN_NOTIFICATIONS_SUBJECT_PREFIX ?? 'bagel.rpc.admin.notifications'
};

export const STATUS_PREFIX = SUB.status;

// Scope -> cache key routing for the invalidation bus, declared as data.
// user:<id> also covers user-login: only via the coarse 'users:'-adjacent
// prefixes below; login-keyed lookups decay by policy (5s fresh).
// commands/modules/delegation fire on every dashboard save and admin caches
// none of that data — explicit no-ops so they don't churn user keys.
const SCOPES: ScopeMap = {
  status: (id) => ['users:', `user:${id}`, `token:${id}`],
  grant: (id) => ['users:', `user:${id}`, `token:${id}`],
  staff: () => ['staff:', 'auth:'],
  commands: () => [],
  modules: () => [],
  delegation: () => [],
  notifications: () => ['notifications:'],
  '*': (id) => ['users:', `user:${id}`, `token:${id}`]
};

// Hybrid read path facade: L1 SwrCache with push invalidation + SWR. Admin data
// has no Valkey projection, so reads are L1 -> RPC. Policies from the shared
// table keep the operator view ≤5s/≤3s stale while SWR makes repeat loads instant.
const fabric = createCacheFabric({
  app: 'admin',
  scopes: SCOPES,
  capacity: adminL1CacheCapacity(process.env)
});

function cached<T>(key: string, policy: CachePolicy, load: () => Promise<T>): Promise<T> {
  return fabric.readKey(key, policy, load);
}

function setCached<T>(key: string, value: T, policy: CachePolicy) {
  fabric.cache.set(key, value, policy);
}

function invalidate(...prefixes: string[]) {
  fabric.invalidate(...prefixes);
}

function invalidateUser(userId: string) {
  invalidate('users:', `user:${userId}`, `token:${userId}`);
}

// Fire-and-forget cross-replica cache-invalidation publish. Local invalidation
// already ran synchronously; this just tells OTHER replicas to evict their
// own in-process caches for the same scope.
function broadcastInvalidate(scope: string, broadcasterId: string) {
  void publish(`${getServerConfig().cacheInvalidationPrefix}.${scope}`, {
    broadcaster_id: broadcasterId
  }).catch(() => {});
}

// AdminUserWire mirrors the users service's admin wire format (broadcaster-data):
// numeric id, raw status enum, activity flag, last-update timestamp.
export interface AdminUserWire {
  id: number;
  username: string;
  is_active: boolean;
  status: string; // "free" | "paid" | "vip"
  banned: boolean;
  creator_code?: string | null;
  subscription_expires_at?: string;
  subscription_source?: string;
  subscription_ref?: string;
  updated_at?: string;
}

export interface TokenStatus {
  present: boolean;
}

export interface UserPage {
  users: AdminUserWire[];
  stats: UserStats;
  page: number;
  page_size: number;
  max_pages: number;
  has_more: boolean;
}

// ── Shards ──────────────────────────────────────────────────────────────────

export const shardSnapshot = defineRead({
  subject: SUB.shards,
  request: () => ({}),
  map: (reply: ShardSnapshot) => reply,
  timeoutMs: 5000,
  cache: {
    fabric,
    key: () => 'shards:snapshot',
    policy: POLICY.live
  }
});

export const shardScale = defineWrite({
  subject: SUB.scale,
  request: (count: number) => ({ count }),
  map: (reply: ShardSnapshot) => reply,
  timeoutMs: 5000,
  after: (snapshot) => setCached('shards:snapshot', snapshot, POLICY.live)
});

export const shardAutoscale = defineWrite({
  subject: SUB.autoscale,
  request: (enabled: boolean) => ({ enabled }),
  map: (reply: ShardSnapshot) => reply,
  timeoutMs: 5000,
  after: (snapshot) => setCached('shards:snapshot', snapshot, POLICY.live)
});

// ── Users ───────────────────────────────────────────────────────────────────

function isDigits(s: string): boolean {
  return /^[0-9]+$/.test(s);
}

// Dual-key lookup (numeric id vs. login) plus a write-through side-set of the
// canonical user:<id> key on a login hit — the factory's single cache-key shape
// doesn't fit this cleanly, so it stays hand-written.
export async function userLookup(q: string): Promise<AdminUserWire> {
  const req = isDigits(q) ? { user_id: q } : { username: q };
  const key = isDigits(q) ? `user:${q}` : `user-login:${q.toLowerCase()}`;
  return cached(key, POLICY.adminRead, async () => {
    const r = await rpc<{ user: AdminUserWire }>(`${SUB.user}.get`, req);
    if (r.user) setCached(`user:${r.user.id}`, r.user, POLICY.adminRead);
    return r.user;
  });
}

export const userList = defineRead({
  subject: `${SUB.user}.list`,
  request: (limit = 20) => ({ limit }),
  map: (reply: { users: AdminUserWire[] }) => reply.users ?? [],
  cache: {
    fabric,
    key: (limit = 20) => `users:list:${limit}`,
    policy: POLICY.adminPage
  }
});

export const userStats = defineRead({
  subject: `${SUB.user}.stats`,
  request: () => ({}),
  map: (reply: { stats: UserStats }) => reply.stats,
  cache: {
    fabric,
    key: () => 'users:stats',
    policy: POLICY.adminPage
  }
});

// EnrollmentWire mirrors the users service's admin enrollment reply: one
// zero-filled bucket per UTC day plus the current user totals.
export interface EnrollmentDayWire {
  date: string; // YYYY-MM-DD
  count: number;
}

export interface EnrollmentWire {
  days: EnrollmentDayWire[];
  stats: UserStats;
}

export const ENROLLMENT_WINDOW_DAYS = 30;

export const userEnrollment = defineRead({
  subject: `${SUB.user}.enrollment`,
  request: (days = ENROLLMENT_WINDOW_DAYS) => ({ days }),
  map: (reply: { enrollment: EnrollmentWire }) => reply.enrollment,
  cache: {
    fabric,
    key: (days = ENROLLMENT_WINDOW_DAYS) => `users:enrollment:${days}`,
    policy: POLICY.adminPage
  }
});

export const USER_PAGE_SIZE = 15;
export const USER_MAX_PAGES = 25;

// Hand-written, not defineRead: the reply's page/page_size/max_pages fields
// fall back to the request args (page, USER_PAGE_SIZE, USER_MAX_PAGES) when the
// responder omits them, and defineRead's `map` only sees the reply, not args.
export async function userOverview(page = 1, search = ''): Promise<UserPage> {
  return cached(`users:overview:${page}:${search}`, POLICY.adminPage, async () => {
    const r = await rpc<{
      users?: AdminUserWire[];
      stats: UserStats;
      page?: number;
      page_size?: number;
      max_pages?: number;
      has_more?: boolean;
    }>(`${SUB.user}.overview`, {
      page,
      limit: USER_PAGE_SIZE,
      search
    });
    return {
      users: r.users ?? [],
      stats: r.stats,
      page: r.page ?? page,
      page_size: r.page_size ?? USER_PAGE_SIZE,
      max_pages: r.max_pages ?? USER_MAX_PAGES,
      has_more: Boolean(r.has_more)
    };
  });
}

export const userSetStatus = defineWrite({
  subject: `${SUB.user}.set_status`,
  // expiresAt (ISO timestamp) is required by the users service when status is
  // "paid": every operator grant carries the day it ends.
  request: (userId: string, status: string, expiresAt?: string) => ({
    user_id: userId,
    status,
    ...(expiresAt ? { expires_at: expiresAt } : {})
  }),
  map: (reply: { user: AdminUserWire }) => reply.user,
  after: (user, userId) => {
    invalidateUser(userId);
    setCached(`user:${user.id}`, user, POLICY.adminRead);
  }
});

export const userReset = defineWrite({
  subject: `${SUB.user}.reset`,
  request: (userId: string) => ({ user_id: userId }),
  map: (reply: { user: AdminUserWire }) => reply.user,
  after: (user, userId) => {
    invalidateUser(userId);
    setCached(`user:${user.id}`, user, POLICY.adminRead);
  }
});

export const tokenStatus = defineRead({
  subject: `${SUB.user}.token_status`,
  request: (userId: string) => ({ user_id: userId }),
  map: (reply: { token: TokenStatus }) => reply.token ?? { present: false },
  cache: {
    fabric,
    key: (userId: string) => `token:${userId}`,
    policy: POLICY.adminRead
  }
});

export const tokenSet = defineWrite({
  subject: `${SUB.user}.token_set`,
  request: (userId: string, accessToken: string, refreshToken: string) => ({
    user_id: userId,
    access_token: accessToken,
    refresh_token: refreshToken
  }),
  map: (reply: { token: TokenStatus }) => reply.token ?? { present: false },
  after: (token, userId) => setCached(`token:${userId}`, token, POLICY.adminRead)
});

export const tokenClear = defineWrite({
  subject: `${SUB.user}.token_clear`,
  request: (userId: string) => ({ user_id: userId }),
  map: (reply: { token: TokenStatus }) => reply.token ?? { present: false },
  after: (token, userId) => setCached(`token:${userId}`, token, POLICY.adminRead)
});

export async function userDelete(userId: string): Promise<void> {
  const r = await rpc<{ error?: string }>(`${SUB.user}.delete`, { user_id: userId });
  if (r.error) throw new Error(r.error);
  invalidateUser(userId);
}

export const userSetActive = defineWrite({
  subject: `${SUB.user}.set_active`,
  request: (userId: string, active: boolean) => ({ user_id: userId, active }),
  map: (reply: { user: AdminUserWire }) => reply.user,
  after: (user, userId) => {
    invalidateUser(userId);
    setCached(`user:${user.id}`, user, POLICY.adminRead);
  }
});

export const userSetCreatorCode = defineWrite({
  subject: `${SUB.user}.set_creator_code`,
  request: (userId: string, creatorCode: string) => ({
    user_id: userId,
    creator_code: creatorCode
  }),
  map: (reply: { user: AdminUserWire }) => reply.user,
  after: (user, userId) => {
    invalidateUser(userId);
    setCached(`user:${user.id}`, user, POLICY.adminRead);
  }
});

export const userBan = defineWrite({
  subject: `${SUB.user}.ban`,
  request: (userId: string) => ({ user_id: userId }),
  map: (reply: { user: AdminUserWire }) => reply.user,
  after: (user, userId) => {
    invalidateUser(userId);
    setCached(`user:${user.id}`, user, POLICY.adminRead);
  }
});

export const userUnban = defineWrite({
  subject: `${SUB.user}.unban`,
  request: (userId: string) => ({ user_id: userId }),
  map: (reply: { user: AdminUserWire }) => reply.user,
  after: (user, userId) => {
    invalidateUser(userId);
    setCached(`user:${user.id}`, user, POLICY.adminRead);
  }
});

export async function restartUserEventSub(userId: string): Promise<void> {
  await publish(SUB.outgress, { type: 'eventsub', broadcaster_id: userId, payload: { mode: 'reconnect' } });
  invalidateUser(userId);
}

export type ChannelSubState = {
  state: 'ok' | 'pending' | 'failing' | 'unknown';
  error: string;
  checkedAt: string | null;
};

// Read the persisted EventSub enroll state for a channel. Fails safe: returns
// 'unknown' on RPC error so a transient outage never blocks page render. The
// try/catch fail-open semantics don't fit defineRead's throw-on-error contract,
// so this stays hand-written.
export async function channelSubState(broadcasterId: string): Promise<ChannelSubState> {
  try {
    const r = await rpc<{
      found: boolean;
      channel?: { sub_state: string; sub_error: string; sub_checked_at: string };
    }>(`${SUB.outgressRpc}.channel.get`, { broadcaster_id: broadcasterId }, 2000);
    const c = r.channel;
    if (!r.found || !c) return { state: 'unknown', error: '', checkedAt: null };
    const s = (c.sub_state || '') as string;
    const state = (s === 'ok' || s === 'pending' || s === 'failing') ? s : 'unknown';
    return { state, error: c.sub_error || '', checkedAt: c.sub_checked_at || null };
  } catch {
    return { state: 'unknown', error: '', checkedAt: null };
  }
}

// ── Notifications ────────────────────────────────────────────────────────────

export interface NotificationWire {
  id: number;
  scope: 'broadcast' | 'direct';
  title: string;
  body: string;
  level: 'info' | 'success' | 'warning' | 'critical';
  target_user_id?: number;
  created_by_login: string;
  created_at: string;
  expires_at?: string;
  read: boolean;
}

export interface NotificationPage {
  notifications: NotificationWire[];
  page: number;
  page_size: number;
  max_pages: number;
  has_more: boolean;
}

export const NOTIFICATIONS_PAGE_SIZE = 20;
export const NOTIFICATIONS_MAX_PAGES = 25;

export const notificationsList = defineRead({
  subject: `${SUB.notifications}.list`,
  request: (page = 1) => ({ page, limit: NOTIFICATIONS_PAGE_SIZE }),
  map: (reply: {
    notifications?: NotificationWire[];
    page?: number;
    page_size?: number;
    max_pages?: number;
    has_more?: boolean;
  }): NotificationPage => ({
    notifications: reply.notifications ?? [],
    page: reply.page ?? 1,
    page_size: reply.page_size ?? NOTIFICATIONS_PAGE_SIZE,
    max_pages: reply.max_pages ?? NOTIFICATIONS_MAX_PAGES,
    has_more: Boolean(reply.has_more)
  }),
  cache: {
    fabric,
    key: (page = 1) => `notifications:list:${page}`,
    policy: POLICY.adminPage
  }
});

export const notificationSend = defineWrite({
  subject: `${SUB.notifications}.send`,
  request: (params: {
    scope: 'broadcast' | 'direct';
    targetUserId?: string;
    targetUsername?: string;
    title: string;
    body: string;
    level: string;
    expiresAt?: string;
    actorId: string;
    actorLogin: string;
  }) => ({
    scope: params.scope,
    target_user_id: params.targetUserId ?? '',
    target_username: params.targetUsername ?? '',
    title: params.title,
    body: params.body,
    level: params.level,
    expires_at: params.expiresAt || undefined,
    actor_id: params.actorId,
    actor_login: params.actorLogin,
    // One value per logical send. If NATS transports the request over more
    // than one route, every delivery carries the same database idempotency key.
    request_id: crypto.randomUUID()
  }),
  map: (reply: { notification: NotificationWire }) => reply.notification,
  after: () => invalidate('notifications:')
});

export async function notificationDelete(id: number): Promise<void> {
  const r = await rpc<{ error?: string }>(`${SUB.notifications}.delete`, { id });
  if (r.error) throw new Error(r.error);
  invalidate('notifications:');
}

// ── Cache invalidation listener ───────────────────────────────────────────────

/**
 * Subscribe to the cache-invalidation bus so writes in other services push-drop
 * affected keys without waiting on TTL expiry. Call once at server boot
 * (hooks.server.ts init). Scope -> key routing is the SCOPES map above; the
 * shared router owns transport, parsing, retry, and gap flushes.
 */
export function startInvalidationListener(): void {
  fabric.start();
}

// ── Derived helpers ───────────────────────────────────────────────────────────

export function tierOf(status: string): 'premium' | 'standard' {
  return status === 'paid' || status === 'vip' ? 'premium' : 'standard';
}

// ── Admin auth + audit ────────────────────────────────────────────────────────
// DB-backed (users service) replacement for the old static ADMIN_USER_IDS env
// allowlist. auth.check decides who may operate; audit.* records what they did.

export type AdminRole = 'moderator' | 'admin' | 'owner';

export interface AdminCheck {
  admin: boolean;
  role?: AdminRole;
  login?: string;
  display_name?: string;
}

export interface AdminAcct {
  id: number;
  login: string;
  display_name: string;
  role: AdminRole;
  active: boolean;
  added_by: number;
  created_at: string;
}

export interface AuditEntry {
  id: number;
  actor_id: number;
  actor_login: string;
  action: string;
  target?: string;
  detail?: string;
  ok: boolean;
  error?: string;
  created_at: string;
}

export interface AuditPage {
  entries: AuditEntry[];
  page: number;
  page_size: number;
  max_pages: number;
  has_more: boolean;
}

export const AUDIT_PAGE_SIZE = 15;
export const AUDIT_MAX_PAGES = 25;

// adminCheck resolves whether a Twitch subject is an active admin. Login/display
// are passed through so the allowlist self-heals after a Twitch rename.
export const adminCheck = defineRead({
  subject: `${SUB.auth}.check`,
  request: (userId: string, login?: string, displayName?: string) => ({
    user_id: userId,
    login: login ?? '',
    display_name: displayName ?? ''
  }),
  map: (reply: AdminCheck) => reply,
  cache: {
    fabric,
    // Cached by subject id only; login/display are pass-through self-heal hints.
    key: (userId: string, _login?: string, _displayName?: string) => `auth:${userId}`,
    policy: POLICY.adminRead
  }
});

export const adminListAccts = defineRead({
  subject: `${SUB.auth}.list`,
  request: () => ({}),
  map: (reply: { admins?: AdminAcct[] }) => reply.admins ?? [],
  cache: {
    fabric,
    key: () => 'staff:list',
    policy: POLICY.adminPage
  }
});

// staffUpsert creates or modifies a staff member. The actor (id + role) is
// carried so the users service can enforce the role ladder server-side.
export async function staffUpsert(
  actor: { id: string; role: AdminRole },
  target: { userId: string; login: string; displayName: string; role: AdminRole }
): Promise<AdminAcct[]> {
  const r = await rpc<{ admins?: AdminAcct[]; error?: string }>(`${SUB.auth}.upsert`, {
    actor_id: actor.id,
    actor_role: actor.role,
    user_id: target.userId,
    login: target.login,
    display_name: target.displayName,
    role: target.role
  });
  if (r.error) throw new Error(r.error);
  invalidate('staff:', 'auth:');
  broadcastInvalidate('staff', target.userId);
  return r.admins ?? [];
}

export async function staffRemove(
  actor: { id: string; role: AdminRole },
  userId: string
): Promise<AdminAcct[]> {
  const r = await rpc<{ admins?: AdminAcct[]; error?: string }>(`${SUB.auth}.remove`, {
    actor_id: actor.id,
    actor_role: actor.role,
    user_id: userId
  });
  if (r.error) throw new Error(r.error);
  invalidate('staff:', 'auth:');
  broadcastInvalidate('staff', userId);
  return r.admins ?? [];
}

// auditAppend is best-effort: a logging failure must never block the operator
// action it records, so callers fire-and-forget and swallow errors.
export async function auditAppend(entry: {
  actor_id: string;
  actor_login: string;
  action: string;
  target?: string;
  detail?: string;
  ok: boolean;
  error?: string;
}): Promise<void> {
  await rpc(`${SUB.audit}.append`, {
    actor_id: entry.actor_id,
    actor_login: entry.actor_login,
    action: entry.action,
    target: entry.target ?? '',
    detail: entry.detail ?? '',
    ok: entry.ok,
    error: entry.error ?? ''
  });
  invalidate('audit:');
}

// auditList returns the newest entries, optionally scoped to one actor's id so
// a member's history can be lazy-loaded without shipping the whole log.
export const auditList = defineRead({
  subject: `${SUB.audit}.list`,
  request: (limit = 50, actorId?: string) => ({
    limit,
    actor_filter: actorId ?? ''
  }),
  map: (reply: { entries?: AuditEntry[] }) => reply.entries ?? [],
  cache: {
    fabric,
    key: (limit = 50, actorId?: string) => `audit:list:${limit}:${actorId ?? ''}`,
    policy: POLICY.adminPage
  }
});

// Hand-written, not defineRead: same reply-falls-back-to-args shape as
// userOverview above (page/page_size/max_pages default from the call args).
export async function auditPage(page = 1, search = '', actorFilter = ''): Promise<AuditPage> {
  return cached(`audit:page:${page}:${search}:${actorFilter}`, POLICY.adminPage, async () => {
    const r = await rpc<{
      entries?: AuditEntry[];
      page?: number;
      page_size?: number;
      max_pages?: number;
      has_more?: boolean;
    }>(`${SUB.audit}.list`, {
      page,
      limit: AUDIT_PAGE_SIZE,
      search,
      actor_filter: actorFilter
    });

    return {
      entries: r.entries ?? [],
      page: r.page ?? page,
      page_size: r.page_size ?? AUDIT_PAGE_SIZE,
      max_pages: r.max_pages ?? AUDIT_MAX_PAGES,
      has_more: Boolean(r.has_more)
    };
  });
}
