// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import { rpc, publish, subscribeDurable, RpcError } from '@bagel/kit/server/nats';
import { codeReader } from '@bagel/kit/server/rpc-code';
import { defineRead, defineWrite } from '@bagel/kit/server/service';
import { createCacheFabric } from '@bagel/kit/server/cache-fabric';
import { POLICY, type CachePolicy } from '@bagel/kit/server/cache-keys';
import { getServerConfig } from '@bagel/kit/server/config';
import type { ScopeMap } from '@bagel/kit/server/invalidation';
import type { ShardSnapshot, UserStats } from '@bagel/kit';
import { adminL1CacheCapacity } from './config-sanity';
import {
  DEPLOY_EVENTS_PREFIX,
  DEPLOY_PREFIX,
  type DeployPlan,
  type DeployRun,
  type DeployRunSummary,
  type ListReply,
  type ListRequest,
  type PlanReply,
  type PlanRequest,
  type RunReply,
  type RunRequest,
  type StartRequest
} from '$lib/deploys/types';

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
// `||`, not `??`: a set-but-blank env var would build subjects no responder answers.
const SUB = {
  shards: process.env.NATS_ADMIN_SUBJECT || 'twitch.ingress.admin.shards.get',
  scale: process.env.NATS_SHARD_SCALE_SUBJECT || 'twitch.ingress.admin.shards.scale',
  autoscale: process.env.NATS_SHARD_AUTOSCALE_SUBJECT || 'twitch.ingress.admin.shards.autoscale',
  trials: process.env.NATS_TRIAL_SUBJECT_PREFIX || 'twitch.ingress.admin.trials',
  status: process.env.NATS_STATUS_SUBJECT_PREFIX || 'twitch.ingress.status',
  user: process.env.NATS_ADMIN_USER_SUBJECT_PREFIX || 'bagel.rpc.admin.user',
  auth: process.env.NATS_ADMIN_AUTH_SUBJECT_PREFIX || 'bagel.rpc.admin.user.auth',
  audit: process.env.NATS_ADMIN_AUDIT_SUBJECT_PREFIX || 'bagel.rpc.admin.user.audit',
  outgress: process.env.NATS_OUTGRESS_SYSTEM_SUBJECT || 'twitch.outgress.system',
  outgressRpc: process.env.NATS_OUTGRESS_RPC_PREFIX || 'bagel.rpc.outgress',
  notifications: process.env.NATS_ADMIN_NOTIFICATIONS_SUBJECT_PREFIX || 'bagel.rpc.admin.notifications',
  loyalty: process.env.NATS_LOYALTY_SUBJECT_PREFIX || 'bagel.rpc.loyalty',
  health: process.env.NATS_RPC_HEALTH_PREFIX || 'bagel.rpc.health',
  deploy: process.env.NATS_ADMIN_DEPLOY_SUBJECT_PREFIX || DEPLOY_PREFIX,
  deployEvents: process.env.NATS_DEPLOY_EVENTS_SUBJECT_PREFIX || DEPLOY_EVENTS_PREFIX
};

export const STATUS_PREFIX = SUB.status;

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

function refreshUser(user: AdminUserWire, ref: UserRef) {
  invalidateUser(ref.userId);
  setCached(`user:${user.id}`, user, POLICY.adminRead);
}

function broadcastInvalidate(scope: string, broadcasterId: string) {
  void publish(`${getServerConfig().cacheInvalidationPrefix}.${scope}`, {
    broadcaster_id: broadcasterId
  }).catch(() => {});
}

export interface UserRef {
  actorId: string;
  userId: string;
}

export interface AdminUserWire {
  id: number;
  username: string;
  is_active: boolean;
  status: string;
  banned: boolean;
  creator_code?: string | null;
  subscription_expires_at?: string;
  subscription_source?: string;
  subscription_ref?: string;
  subscription_cancel_pending?: boolean;
  created_at?: string;
  updated_at?: string;
  test_account?: boolean;
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

export interface TrialChannel {
  broadcaster_id: string;
  display_name?: string;
  enabled: boolean;
  state: 'pending' | 'receiving' | 'disabled' | 'stopping' | 'promoted' | 'removed' | 'failed';
  slot?: number;
  error?: string;
  received?: number;
  decoded?: number;
  processed?: number;
  failed?: number;
  retried?: number;
  blocked_actions?: number;
  average_processing_latency_ms?: number;
}

export interface TrialSnapshot {
  version: number;
  active_count?: number;
  max_channels?: number;
  socket_target?: number;
  trials: TrialChannel[];
}

export type TrialMutationReply = Pick<TrialChannel, 'broadcaster_id' | 'state'> & Partial<Pick<TrialChannel, 'enabled'>>;

export async function trialList(): Promise<TrialSnapshot> {
  const snapshot = await rpc<TrialSnapshot>(`${SUB.trials}.list`, {});
  const counts = await Promise.all(snapshot.trials.map((row) => trialCounters(row.broadcaster_id)));
  return { ...snapshot, trials: snapshot.trials.map((row, i) => withTrialCounters(row, counts[i])) };
}

function trialCounters(broadcasterId: string): Promise<Map<string, number> | null> {
  return rpc<{ counters?: { name: string; value: number }[] }>(`${SUB.loyalty}.counter.trial`, { user_id: broadcasterId })
    .then((reply) => new Map((reply.counters ?? []).map((c) => [c.name, c.value])))
    .catch(() => null);
}

function withTrialCounters(row: TrialChannel, counts: Map<string, number> | null): TrialChannel {
  if (!counts) return row;
  const count = (name: string) => counts.get(`trial_${name}`) ?? 0;
  const samples = count('latency_samples');
  return {
    ...row,
    decoded: count('decoded'),
    processed: count('processed'),
    failed: count('failed') + (row.failed ?? 0),
    retried: count('retried'),
    blocked_actions: count('blocked'),
    average_processing_latency_ms: samples ? Math.round(count('latency_total_ms') / samples) : 0
  };
}

export function trialAdd(broadcasterId: string): Promise<TrialMutationReply> {
  return rpc<TrialMutationReply>(`${SUB.trials}.add`, { broadcaster_id: broadcasterId });
}

export function trialRemove(broadcasterId: string): Promise<TrialMutationReply> {
  return rpc<TrialMutationReply>(`${SUB.trials}.remove`, { broadcaster_id: broadcasterId });
}

export function trialSetEnabled(broadcasterId: string, enabled: boolean): Promise<TrialMutationReply> {
  return rpc<TrialMutationReply>(`${SUB.trials}.set_enabled`, { broadcaster_id: broadcasterId, enabled });
}

const readRefusal = codeReader(['forbidden'] as const);

export function isForbidden(e: unknown): boolean {
  if (!(e instanceof RpcError)) return false;
  return readRefusal({ code: e.code, error: e.message }) === 'forbidden';
}

function isDigits(s: string): boolean {
  return /^[0-9]+$/.test(s);
}

export async function userLookup(actorId: string, q: string): Promise<AdminUserWire> {
  const req = isDigits(q) ? { actor_id: actorId, user_id: q } : { actor_id: actorId, username: q };
  const key = isDigits(q) ? `user:${q}` : `user-login:${q.toLowerCase()}`;
  return cached(key, POLICY.adminRead, async () => {
    const r = await rpc<{ user: AdminUserWire }>(`${SUB.user}.get`, req);
    if (r.user) setCached(`user:${r.user.id}`, r.user, POLICY.adminRead);
    return r.user;
  });
}

export const userList = defineRead({
  subject: `${SUB.user}.list`,
  request: (actorId: string, limit = 20) => ({ actor_id: actorId, limit }),
  map: (reply: { users: AdminUserWire[] }) => reply.users ?? [],
  cache: {
    fabric,
    key: (_actorId: string, limit = 20) => `users:list:${limit}`,
    policy: POLICY.adminPage
  }
});

export const userStats = defineRead({
  subject: `${SUB.user}.stats`,
  request: (actorId: string) => ({ actor_id: actorId }),
  map: (reply: { stats: UserStats }) => reply.stats,
  cache: {
    fabric,
    key: () => 'users:stats',
    policy: POLICY.adminPage
  }
});

export interface EnrollmentDayWire {
  date: string;
  count: number;
}

export interface EnrollmentWire {
  days: EnrollmentDayWire[];
  stats: UserStats;
}

export const ENROLLMENT_WINDOW_DAYS = 30;

export const userEnrollment = defineRead({
  subject: `${SUB.user}.enrollment`,
  request: (actorId: string, days = ENROLLMENT_WINDOW_DAYS) => ({ actor_id: actorId, days }),
  map: (reply: { enrollment: EnrollmentWire }) => reply.enrollment,
  cache: {
    fabric,
    key: (_actorId: string, days = ENROLLMENT_WINDOW_DAYS) => `users:enrollment:${days}`,
    policy: POLICY.adminPage
  }
});

export const USER_PAGE_SIZE = 15;
export const USER_MAX_PAGES = 25;

interface PageMetaWire {
  page?: number;
  page_size?: number;
  max_pages?: number;
  has_more?: boolean;
}

interface PageMeta {
  page: number;
  page_size: number;
  max_pages: number;
  has_more: boolean;
}

type PageWindow = { page: number; pageSize: number; maxPages: number };

function pageMetaOf(reply: PageMetaWire, fallback: PageWindow): PageMeta {
  return {
    page: reply.page ?? fallback.page,
    page_size: reply.page_size ?? fallback.pageSize,
    max_pages: reply.max_pages ?? fallback.maxPages,
    has_more: Boolean(reply.has_more)
  };
}

export async function userOverview(
  actorId: string,
  page = 1,
  search = '',
  state = ''
): Promise<UserPage> {
  return cached(`users:overview:${page}:${search}:${state}`, POLICY.adminPage, async () => {
    const r = await rpc<PageMetaWire & { users?: AdminUserWire[]; stats: UserStats }>(
      `${SUB.user}.overview`,
      { actor_id: actorId, page, limit: USER_PAGE_SIZE, search, state }
    );
    return {
      users: r.users ?? [],
      stats: r.stats,
      ...pageMetaOf(r, { page, pageSize: USER_PAGE_SIZE, maxPages: USER_MAX_PAGES })
    };
  });
}

export const userSetStatus = defineWrite({
  subject: `${SUB.user}.set_status`,
  request: (ref: UserRef, status: string, expiresAt?: string) => ({
    actor_id: ref.actorId,
    user_id: ref.userId,
    status,
    ...(expiresAt ? { expires_at: expiresAt } : {})
  }),
  map: (reply: { user: AdminUserWire }) => reply.user,
  after: (user, ref) => refreshUser(user, ref)
});

export const userReset = defineWrite({
  subject: `${SUB.user}.reset`,
  request: (ref: UserRef) => ({ actor_id: ref.actorId, user_id: ref.userId }),
  map: (reply: { user: AdminUserWire }) => reply.user,
  after: (user, ref) => refreshUser(user, ref)
});

export const tokenStatus = defineRead({
  subject: `${SUB.user}.token_status`,
  request: (ref: UserRef) => ({ actor_id: ref.actorId, user_id: ref.userId }),
  map: (reply: { token: TokenStatus }) => reply.token ?? { present: false },
  cache: {
    fabric,
    key: (ref: UserRef) => `token:${ref.userId}`,
    policy: POLICY.adminRead
  }
});

export const tokenSet = defineWrite({
  subject: `${SUB.user}.token_set`,
  request: (ref: UserRef, accessToken: string, refreshToken: string) => ({
    actor_id: ref.actorId,
    user_id: ref.userId,
    access_token: accessToken,
    refresh_token: refreshToken
  }),
  map: (reply: { token: TokenStatus }) => reply.token ?? { present: false },
  after: (token, ref) => setCached(`token:${ref.userId}`, token, POLICY.adminRead)
});

export const tokenClear = defineWrite({
  subject: `${SUB.user}.token_clear`,
  request: (ref: UserRef) => ({ actor_id: ref.actorId, user_id: ref.userId }),
  map: (reply: { token: TokenStatus }) => reply.token ?? { present: false },
  after: (token, ref) => setCached(`token:${ref.userId}`, token, POLICY.adminRead)
});

export async function userDelete(ref: UserRef): Promise<void> {
  const r = await rpc<{ error?: string }>(`${SUB.user}.delete`, {
    actor_id: ref.actorId,
    user_id: ref.userId
  });
  if (r.error) throw new Error(r.error);
  invalidateUser(ref.userId);
}

type UserBooleanSetting = { subject: string; field: 'active' | 'test_account' };

function userSetBoolean(setting: UserBooleanSetting) {
  return defineWrite({
    subject: setting.subject,
    request: (ref: UserRef, enabled: boolean) => ({ actor_id: ref.actorId, user_id: ref.userId, [setting.field]: enabled }),
    map: (reply: { user: AdminUserWire }) => reply.user,
    after: (user, ref) => refreshUser(user, ref)
  });
}

export const userSetActive = userSetBoolean({ subject: `${SUB.user}.set_active`, field: 'active' });

export const userSetTestAccount = userSetBoolean({ subject: 'bagel.rpc.admin.user.test.set', field: 'test_account' });

export const userSetCreatorCode = defineWrite({
  subject: `${SUB.user}.set_creator_code`,
  request: (ref: UserRef, creatorCode: string) => ({
    actor_id: ref.actorId,
    user_id: ref.userId,
    creator_code: creatorCode
  }),
  map: (reply: { user: AdminUserWire }) => reply.user,
  after: (user, ref) => refreshUser(user, ref)
});

export const userBan = defineWrite({
  subject: `${SUB.user}.ban`,
  request: (ref: UserRef) => ({ actor_id: ref.actorId, user_id: ref.userId }),
  map: (reply: { user: AdminUserWire }) => reply.user,
  after: (user, ref) => refreshUser(user, ref)
});

export const userUnban = defineWrite({
  subject: `${SUB.user}.unban`,
  request: (ref: UserRef) => ({ actor_id: ref.actorId, user_id: ref.userId }),
  map: (reply: { user: AdminUserWire }) => reply.user,
  after: (user, ref) => refreshUser(user, ref)
});

export async function restartUserEventSub(userId: string): Promise<void> {
  await publish(SUB.outgress, { type: 'eventsub', broadcaster_id: userId, payload: { mode: 'reconnect' } });
  invalidateUser(userId);
}

export async function publishUserEventSub(userId: string, enabled: boolean): Promise<void> {
  await publish(SUB.outgress, { type: 'eventsub', broadcaster_id: userId, payload: { enabled } });
}

export type ChannelSubState = {
  state: 'ok' | 'pending' | 'failing' | 'revoked' | 'chat_banned' | 'unknown';
  error: string;
  checkedAt: string | null;
};

export async function channelSubState(broadcasterId: string): Promise<ChannelSubState> {
  try {
    const r = await rpc<{
      found: boolean;
      channel?: { sub_state: string; sub_error: string; sub_checked_at: string };
    }>(`${SUB.outgressRpc}.channel.get`, { broadcaster_id: broadcasterId }, 2000);
    const c = r.channel;
    if (!r.found || !c) return { state: 'unknown', error: '', checkedAt: null };
    const s = (c.sub_state || '') as string;
    const known = ['ok', 'pending', 'failing', 'revoked', 'chat_banned'];
    const state = known.includes(s) ? (s as ChannelSubState['state']) : 'unknown';
    return { state, error: c.sub_error || '', checkedAt: c.sub_checked_at || null };
  } catch {
    return { state: 'unknown', error: '', checkedAt: null };
  }
}

export interface ServiceHealth {
  id: string;
  label: string;
  ok: boolean;
  ms: number;
  error?: string;
}

const HEALTH_TIMEOUT_MS = 1500;

interface HealthProbe {
  id: string;
  label: string;
  subject: string;
  payload: unknown;
}

const HEALTH_PROBES: HealthProbe[] = [
  { id: 'users', label: 'Users', subject: `${SUB.health}.users`, payload: {} },
  { id: 'commands', label: 'Commands', subject: `${SUB.health}.commands`, payload: {} },
  { id: 'modules', label: 'Modules', subject: `${SUB.health}.modules`, payload: {} },
  { id: 'loyalty', label: 'Loyalty', subject: `${SUB.health}.loyalty`, payload: {} },
  { id: 'projector', label: 'Projector', subject: `${SUB.health}.projector`, payload: {} },
  { id: 'sesame', label: 'Sesame', subject: `${SUB.health}.sesame`, payload: {} },
  { id: 'gossip', label: 'Gossip', subject: `${SUB.health}.gossip`, payload: {} },
  { id: 'ingress', label: 'Ingress', subject: `${SUB.health}.ingress`, payload: {} },
  { id: 'outgress', label: 'Outgress', subject: `${SUB.health}.outgress`, payload: {} },
  { id: 'transactions', label: 'Transactions', subject: `${SUB.health}.transactions`, payload: {} },
  { id: 'notifications', label: 'Notifications', subject: `${SUB.health}.notifications`, payload: {} }
];

async function probeOnce(probe: HealthProbe): Promise<ServiceHealth> {
  const started = performance.now();
  const failure = await rpc(probe.subject, probe.payload, HEALTH_TIMEOUT_MS).then(
    () => undefined,
    (e: unknown) => e instanceof Error ? (e.message || 'unreachable') : String(e || 'unreachable')
  );
  return {
    id: probe.id,
    label: probe.label,
    ok: failure === undefined,
    ms: Math.round(performance.now() - started),
    ...(failure === undefined ? {} : { error: failure })
  };
}

export async function serviceHealth(): Promise<ServiceHealth[]> {
  return Promise.all(HEALTH_PROBES.map(probeOnce));
}

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
  map: (reply: PageMetaWire & { notifications?: NotificationWire[] }): NotificationPage => ({
    notifications: reply.notifications ?? [],
    ...pageMetaOf(reply, { page: 1, pageSize: NOTIFICATIONS_PAGE_SIZE, maxPages: NOTIFICATIONS_MAX_PAGES })
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
    // One id per logical send: it is the database idempotency key across redeliveries.
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

export function startInvalidationListener(): void {
  fabric.start();
}

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

export async function staffUpsert(
  actor: { id: string },
  target: { userId: string; login: string; displayName: string; role: AdminRole }
): Promise<AdminAcct[]> {
  const r = await rpc<{ admins?: AdminAcct[]; error?: string }>(`${SUB.auth}.upsert`, {
    actor_id: actor.id,
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
  actor: { id: string },
  userId: string
): Promise<AdminAcct[]> {
  const r = await rpc<{ admins?: AdminAcct[]; error?: string }>(`${SUB.auth}.remove`, {
    actor_id: actor.id,
    user_id: userId
  });
  if (r.error) throw new Error(r.error);
  invalidate('staff:', 'auth:');
  broadcastInvalidate('staff', userId);
  return r.admins ?? [];
}

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

export async function auditPage(page = 1, search = '', actorFilter = ''): Promise<AuditPage> {
  return cached(`audit:page:${page}:${search}:${actorFilter}`, POLICY.adminPage, async () => {
    const r = await rpc<PageMetaWire & { entries?: AuditEntry[] }>(`${SUB.audit}.list`, {
      page,
      limit: AUDIT_PAGE_SIZE,
      search,
      actor_filter: actorFilter
    });
    return {
      entries: r.entries ?? [],
      ...pageMetaOf(r, { page, pageSize: AUDIT_PAGE_SIZE, maxPages: AUDIT_MAX_PAGES })
    };
  });
}

export interface BotCounter {
  name: string;
  scope: string;
  value: number;
}

const BOT_NS = '0';

interface LoyaltyReplyWire {
  counter?: BotCounter;
  counters?: BotCounter[];
  found?: boolean;
  error?: string;
}

function loyaltyCall(verb: string, req: Record<string, unknown>): Promise<LoyaltyReplyWire> {
  return rpc<LoyaltyReplyWire>(`${SUB.loyalty}.counter.${verb}`, { user_id: BOT_NS, ...req });
}

export async function botCounterList(): Promise<BotCounter[]> {
  const r = await loyaltyCall('list', {});
  if (r.error) throw new Error(r.error);
  return r.counters ?? [];
}

export async function botCounterCreate(name: string): Promise<BotCounter> {
  const r = await loyaltyCall('create', { name, scope: 'bot' });
  if (r.error) throw new Error(r.error);
  if (!r.counter) throw new Error('empty counter reply');
  return r.counter;
}

export async function botCounterSet(name: string, value: number): Promise<void> {
  const r = await loyaltyCall('set', { name, value });
  if (r.error) throw new Error(r.error);
  if (!r.found) throw new Error('unknown counter');
}

export async function botCounterDelete(name: string): Promise<void> {
  const r = await loyaltyCall('delete', { name });
  if (r.error) throw new Error(r.error);
}

// Must match the deployer's DEPLOY_RPC_TIMEOUT.
const DEPLOY_PLAN_TIMEOUT_MS = 20_000;

export type DeployRunId = DeployRun['id'];

export type DeployRuns = { runs: DeployRunSummary[]; activeRunId: DeployRunId | null };

function runOf(reply: RunReply): DeployRun {
  if (!reply.run) throw new Error('deployer replied without a run');
  return reply.run;
}

export const deployPlan = defineRead({
  subject: `${SUB.deploy}.plan`,
  request: (req: PlanRequest) => req,
  map: (reply: PlanReply): DeployPlan | null => reply.plan ?? null,
  timeoutMs: DEPLOY_PLAN_TIMEOUT_MS
});

// A Go nil slice marshals as null: keep the `?? []`.
export const deployList = defineRead({
  subject: `${SUB.deploy}.list`,
  request: (req: ListRequest) => req,
  map: (reply: ListReply): DeployRuns => ({
    runs: reply.runs ?? [],
    activeRunId: reply.active_run_id || null
  })
});

export const deployGet = defineRead({
  subject: `${SUB.deploy}.get`,
  request: (req: RunRequest) => req,
  map: runOf
});

export const deployStart = defineWrite({
  subject: `${SUB.deploy}.start`,
  request: (req: StartRequest) => req,
  map: runOf
});

type DeployRunVerb = 'resume' | 'cancel' | 'approve';

function deployRunWrite(verb: DeployRunVerb) {
  return defineWrite({
    subject: `${SUB.deploy}.${verb}`,
    request: (req: RunRequest) => req,
    map: runOf
  });
}

export const deployResume = deployRunWrite('resume');
export const deployCancel = deployRunWrite('cancel');
export const deployApprove = deployRunWrite('approve');

export type DeployListener = {
  run: (run: DeployRun) => void;
  gap: () => void;
};

// subscribeDurable: plain subscribe gives up for the process lifetime after one dial failure.
const deployWatchers = new Map<DeployRunId, Set<DeployListener>>();
let deployEventsStarted = false;

function deployEvent(subject: string, data: Uint8Array): void {
  const set = deployWatchers.get(subject.slice(SUB.deployEvents.length + 1));
  if (!set) return;
  const run = JSON.parse(new TextDecoder().decode(data)) as DeployRun;
  for (const listener of set) listener.run(run);
}

function deployEventsGap(): void {
  for (const set of deployWatchers.values()) set.forEach((listener) => listener.gap());
}

export function watchDeployRun(runId: DeployRunId, listener: DeployListener): () => void {
  if (!deployEventsStarted) {
    deployEventsStarted = true;
    subscribeDurable(`${SUB.deployEvents}.>`, deployEvent, deployEventsGap);
  }
  const set = deployWatchers.get(runId) ?? new Set<DeployListener>();
  set.add(listener);
  deployWatchers.set(runId, set);
  return () => {
    set.delete(listener);
    if (set.size === 0 && deployWatchers.get(runId) === set) deployWatchers.delete(runId);
  };
}
