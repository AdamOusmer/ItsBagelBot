// Admin-facing RPC wrappers over the shared NATS client. Subjects come from env
// with the same defaults as the retired Go admin tier. Every wrapper degrades
// gracefully: callers catch and fall back to sample data so SSR always renders.
import { rpc } from '@bagel/shared/server/nats';
import type { ShardSnapshot, UserStats } from '@bagel/shared';
import { env } from '$env/dynamic/private';

const SUB = {
  shards: env.NATS_ADMIN_SUBJECT ?? 'twitch.ingress.admin.shards.get',
  status: env.NATS_STATUS_SUBJECT_PREFIX ?? 'twitch.ingress.status',
  user: env.NATS_ADMIN_USER_SUBJECT_PREFIX ?? 'bagel.rpc.admin.user'
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

// ── Derived helpers ───────────────────────────────────────────────────────────

export function tierOf(status: string): 'premium' | 'standard' {
  return status === 'paid' || status === 'vip' ? 'premium' : 'standard';
}
