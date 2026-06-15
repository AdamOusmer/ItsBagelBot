// Admin-facing RPC wrappers over the shared NATS client. Subjects come from env
// with the same defaults as the retired Go admin tier. Every wrapper degrades
// gracefully: callers catch and fall back to sample data so SSR always renders.
import { rpc, publish } from '@bagel/shared/server/nats';
import type { ShardSnapshot, UserStats } from '@bagel/shared';
import { env } from '$env/dynamic/private';

const SUB = {
  shards: env.NATS_ADMIN_SUBJECT ?? 'twitch.ingress.admin.shards.get',
  scale: env.NATS_SHARD_SCALE_SUBJECT ?? 'twitch.ingress.admin.shards.scale',
  autoscale: env.NATS_SHARD_AUTOSCALE_SUBJECT ?? 'twitch.ingress.admin.shards.autoscale',
  status: env.NATS_STATUS_SUBJECT_PREFIX ?? 'twitch.ingress.status',
  user: env.NATS_ADMIN_USER_SUBJECT_PREFIX ?? 'bagel.rpc.admin.user',
  auth: env.NATS_ADMIN_AUTH_SUBJECT_PREFIX ?? 'bagel.rpc.admin.auth',
  audit: env.NATS_ADMIN_AUDIT_SUBJECT_PREFIX ?? 'bagel.rpc.admin.audit',
  outgress: env.NATS_OUTGRESS_SYSTEM_SUBJECT ?? 'twitch.outgress.system'
};

export const STATUS_PREFIX = SUB.status;

// AdminUserWire mirrors the users service's admin wire format (broadcaster-data):
// numeric id, raw status enum, activity flag, last-update timestamp.
export interface AdminUserWire {
  id: number;
  username: string;
  is_active: boolean;
  status: string; // "free" | "paid" | "vip"
  updated_at?: string;
}

export interface TokenStatus {
  present: boolean;
}

// ── Shards ──────────────────────────────────────────────────────────────────

export async function shardSnapshot(): Promise<ShardSnapshot> {
  return rpc<ShardSnapshot>(SUB.shards, {}, 5000);
}

export async function shardScale(count: number): Promise<ShardSnapshot> {
  return rpc<ShardSnapshot>(SUB.scale, { count }, 5000);
}

export async function shardAutoscale(enabled: boolean): Promise<ShardSnapshot> {
  return rpc<ShardSnapshot>(SUB.autoscale, { enabled }, 5000);
}

// ── Users ───────────────────────────────────────────────────────────────────

function isDigits(s: string): boolean {
  return /^[0-9]+$/.test(s);
}

export async function userLookup(q: string): Promise<AdminUserWire> {
  const req = isDigits(q) ? { user_id: q } : { username: q };
  const r = await rpc<{ user: AdminUserWire }>(`${SUB.user}.get`, req);
  return r.user;
}

export async function userList(limit = 20): Promise<AdminUserWire[]> {
  const r = await rpc<{ users: AdminUserWire[] }>(`${SUB.user}.list`, { limit });
  return r.users ?? [];
}

export async function userStats(): Promise<UserStats> {
  const r = await rpc<{ stats: UserStats }>(`${SUB.user}.stats`, {});
  return r.stats;
}

export async function userSetStatus(userId: string, status: string): Promise<AdminUserWire> {
  const r = await rpc<{ user: AdminUserWire }>(`${SUB.user}.set_status`, {
    user_id: userId,
    status
  });
  return r.user;
}

export async function userReset(userId: string): Promise<AdminUserWire> {
  const r = await rpc<{ user: AdminUserWire }>(`${SUB.user}.reset`, { user_id: userId });
  return r.user;
}

export async function tokenStatus(userId: string): Promise<TokenStatus> {
  const r = await rpc<{ token: TokenStatus }>(`${SUB.user}.token_status`, { user_id: userId });
  return r.token ?? { present: false };
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
  return r.token ?? { present: false };
}

export async function tokenClear(userId: string): Promise<TokenStatus> {
  const r = await rpc<{ token: TokenStatus }>(`${SUB.user}.token_clear`, { user_id: userId });
  return r.token ?? { present: false };
}

export async function userDelete(userId: string): Promise<void> {
  const r = await rpc<{ error?: string }>(`${SUB.user}.delete`, { user_id: userId });
  if (r.error) throw new Error(r.error);
}

export async function userSetActive(userId: string, active: boolean): Promise<AdminUserWire> {
  const r = await rpc<{ user: AdminUserWire }>(`${SUB.user}.set_active`, {
    user_id: userId,
    active
  });
  return r.user;
}

export async function restartUserEventSub(userId: string): Promise<void> {
  await publish(SUB.outgress, { type: 'eventsub', broadcaster_id: userId, payload: { enabled: false } });
  await publish(SUB.outgress, { type: 'eventsub', broadcaster_id: userId, payload: { enabled: true } });
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

// adminCheck resolves whether a Twitch subject is an active admin. Login/display
// are passed through so the allowlist self-heals after a Twitch rename.
export async function adminCheck(
  userId: string,
  login?: string,
  displayName?: string
): Promise<AdminCheck> {
  return rpc<AdminCheck>(`${SUB.auth}.check`, {
    user_id: userId,
    login: login ?? '',
    display_name: displayName ?? ''
  });
}

export async function adminListAccts(): Promise<AdminAcct[]> {
  const r = await rpc<{ admins?: AdminAcct[] }>(`${SUB.auth}.list`, {});
  return r.admins ?? [];
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
}

export async function auditList(limit = 50): Promise<AuditEntry[]> {
  const r = await rpc<{ entries?: AuditEntry[] }>(`${SUB.audit}.list`, { limit });
  return r.entries ?? [];
}
