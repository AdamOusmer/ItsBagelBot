// Admin-facing RPC wrappers over the shared NATS client. Subjects come from env
// with the same defaults as the retired Go admin tier. Every wrapper degrades
// gracefully: callers catch and fall back to sample data so SSR always renders.
import { rpc, publish } from '@bagel/shared/server/nats';
import { MemoryCache } from '@bagel/shared/server/cache';
import { startInvalidationBus } from '@bagel/shared/server/invalidation';
import type { ShardSnapshot, UserStats } from '@bagel/shared';

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
  outgressRpc: process.env.NATS_OUTGRESS_RPC_PREFIX ?? 'bagel.rpc.outgress'
};

export const STATUS_PREFIX = SUB.status;

// Tier-1 read cache: bounded LRU + TTL + single-flight (shared primitive). The
// thin cached()/setCached()/invalidate() wrappers keep every call site unchanged.
const cache = new MemoryCache();

function cached<T>(key: string, ttlMs: number, load: () => Promise<T>): Promise<T> {
  return cache.getOrLoad(key, ttlMs, load);
}

function setCached<T>(key: string, value: T, ttlMs: number) {
  cache.set(key, value, ttlMs);
}

function invalidate(...prefixes: string[]) {
  cache.invalidate(...prefixes);
}

function invalidateUser(userId: string) {
  invalidate('users:', `user:${userId}`, `token:${userId}`);
}

const LIVE_TTL_MS = 1_000;
const ADMIN_READ_TTL_MS = 5_000;
const ADMIN_PAGE_TTL_MS = 3_000;

// AdminUserWire mirrors the users service's admin wire format (broadcaster-data):
// numeric id, raw status enum, activity flag, last-update timestamp.
export interface AdminUserWire {
  id: number;
  username: string;
  is_active: boolean;
  status: string; // "free" | "paid" | "vip"
  banned: boolean;
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

export async function shardSnapshot(): Promise<ShardSnapshot> {
  return cached('shards:snapshot', LIVE_TTL_MS, () => rpc<ShardSnapshot>(SUB.shards, {}, 5000));
}

export async function shardScale(count: number): Promise<ShardSnapshot> {
  const snapshot = await rpc<ShardSnapshot>(SUB.scale, { count }, 5000);
  setCached('shards:snapshot', snapshot, LIVE_TTL_MS);
  return snapshot;
}

export async function shardAutoscale(enabled: boolean): Promise<ShardSnapshot> {
  const snapshot = await rpc<ShardSnapshot>(SUB.autoscale, { enabled }, 5000);
  setCached('shards:snapshot', snapshot, LIVE_TTL_MS);
  return snapshot;
}

// ── Users ───────────────────────────────────────────────────────────────────

function isDigits(s: string): boolean {
  return /^[0-9]+$/.test(s);
}

export async function userLookup(q: string): Promise<AdminUserWire> {
  const req = isDigits(q) ? { user_id: q } : { username: q };
  const key = isDigits(q) ? `user:${q}` : `user-login:${q.toLowerCase()}`;
  return cached(key, ADMIN_READ_TTL_MS, async () => {
    const r = await rpc<{ user: AdminUserWire }>(`${SUB.user}.get`, req);
    if (r.user) setCached(`user:${r.user.id}`, r.user, ADMIN_READ_TTL_MS);
    return r.user;
  });
}

export async function userList(limit = 20): Promise<AdminUserWire[]> {
  return cached(`users:list:${limit}`, ADMIN_PAGE_TTL_MS, async () => {
    const r = await rpc<{ users: AdminUserWire[] }>(`${SUB.user}.list`, { limit });
    return r.users ?? [];
  });
}

export async function userStats(): Promise<UserStats> {
  return cached('users:stats', ADMIN_PAGE_TTL_MS, async () => {
    const r = await rpc<{ stats: UserStats }>(`${SUB.user}.stats`, {});
    return r.stats;
  });
}

export const USER_PAGE_SIZE = 15;
export const USER_MAX_PAGES = 25;

export async function userOverview(page = 1, search = ''): Promise<UserPage> {
  return cached(`users:overview:${page}:${search}`, ADMIN_PAGE_TTL_MS, async () => {
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

export async function userSetStatus(userId: string, status: string): Promise<AdminUserWire> {
  const r = await rpc<{ user: AdminUserWire }>(`${SUB.user}.set_status`, {
    user_id: userId,
    status
  });
  invalidateUser(userId);
  setCached(`user:${r.user.id}`, r.user, ADMIN_READ_TTL_MS);
  return r.user;
}

export async function userReset(userId: string): Promise<AdminUserWire> {
  const r = await rpc<{ user: AdminUserWire }>(`${SUB.user}.reset`, { user_id: userId });
  invalidateUser(userId);
  setCached(`user:${r.user.id}`, r.user, ADMIN_READ_TTL_MS);
  return r.user;
}

export async function tokenStatus(userId: string): Promise<TokenStatus> {
  return cached(`token:${userId}`, ADMIN_READ_TTL_MS, async () => {
    const r = await rpc<{ token: TokenStatus }>(`${SUB.user}.token_status`, { user_id: userId });
    return r.token ?? { present: false };
  });
}

export async function tokenSet(
  userId: string,
  accessToken: string,
  refreshToken: string
): Promise<TokenStatus> {
  const r = await rpc<{ token: TokenStatus }>(`${SUB.user}.token_set`, {
    user_id: userId,
    access_token: accessToken,
    refresh_token: refreshToken
  });
  const token = r.token ?? { present: false };
  setCached(`token:${userId}`, token, ADMIN_READ_TTL_MS);
  return token;
}

export async function tokenClear(userId: string): Promise<TokenStatus> {
  const r = await rpc<{ token: TokenStatus }>(`${SUB.user}.token_clear`, { user_id: userId });
  const token = r.token ?? { present: false };
  setCached(`token:${userId}`, token, ADMIN_READ_TTL_MS);
  return token;
}

export async function userDelete(userId: string): Promise<void> {
  const r = await rpc<{ error?: string }>(`${SUB.user}.delete`, { user_id: userId });
  if (r.error) throw new Error(r.error);
  invalidateUser(userId);
}

export async function userSetActive(userId: string, active: boolean): Promise<AdminUserWire> {
  const r = await rpc<{ user: AdminUserWire }>(`${SUB.user}.set_active`, {
    user_id: userId,
    active
  });
  invalidateUser(userId);
  setCached(`user:${r.user.id}`, r.user, ADMIN_READ_TTL_MS);
  return r.user;
}

export async function userBan(userId: string): Promise<AdminUserWire> {
  const r = await rpc<{ user: AdminUserWire }>(`${SUB.user}.ban`, { user_id: userId });
  invalidateUser(userId);
  setCached(`user:${r.user.id}`, r.user, ADMIN_READ_TTL_MS);
  return r.user;
}

export async function userUnban(userId: string): Promise<AdminUserWire> {
  const r = await rpc<{ user: AdminUserWire }>(`${SUB.user}.unban`, { user_id: userId });
  invalidateUser(userId);
  setCached(`user:${r.user.id}`, r.user, ADMIN_READ_TTL_MS);
  return r.user;
}

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
// 'unknown' on RPC error so a transient outage never blocks page render.
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

// ── Cache invalidation listener ───────────────────────────────────────────────

/**
 * Subscribe to the cache-invalidation bus so writes in other services push-drop
 * affected keys without waiting on TTL expiry. Call once at server boot
 * (hooks.server.ts init). The shared bus owns transport + parsing; this maps
 * each scope to the admin's cache keys.
 *
 * Scopes handled:
 *   status | grant -> invalidateUser(broadcasterId) (drops users:, user:<id>, token:<id>)
 *   other          -> ignored (commands/modules/delegation are not cached by admin)
 */
export function startInvalidationListener(): void {
  startInvalidationBus((id, scope) => {
    if (scope === 'status' || scope === 'grant') invalidateUser(id);
  });
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
export async function adminCheck(
  userId: string,
  login?: string,
  displayName?: string
): Promise<AdminCheck> {
  return cached(`auth:${userId}`, ADMIN_READ_TTL_MS, () =>
    rpc<AdminCheck>(`${SUB.auth}.check`, {
      user_id: userId,
      login: login ?? '',
      display_name: displayName ?? ''
    })
  );
}

export async function adminListAccts(): Promise<AdminAcct[]> {
  return cached('staff:list', ADMIN_PAGE_TTL_MS, async () => {
    const r = await rpc<{ admins?: AdminAcct[] }>(`${SUB.auth}.list`, {});
    return r.admins ?? [];
  });
}

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
export async function auditList(limit = 50, actorId?: string): Promise<AuditEntry[]> {
  return cached(`audit:list:${limit}:${actorId ?? ''}`, ADMIN_PAGE_TTL_MS, async () => {
    const r = await rpc<{ entries?: AuditEntry[] }>(`${SUB.audit}.list`, {
      limit,
      actor_filter: actorId ?? ''
    });
    return r.entries ?? [];
  });
}

export async function auditPage(page = 1, search = '', actorFilter = ''): Promise<AuditPage> {
  return cached(`audit:page:${page}:${search}:${actorFilter}`, ADMIN_PAGE_TTL_MS, async () => {
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
