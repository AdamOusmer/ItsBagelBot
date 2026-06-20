// Dashboard-facing RPC wrappers over the shared NATS client. Subjects come from
// env with the same defaults as the retired Go dashboard tier.
import { rpc, publish, subscribe } from '@bagel/shared/server/nats';
import type { CommandView, Perm, Tier } from '@bagel/shared';
import type { Session } from './session';

// Subjects come from process.env, NOT $env/dynamic/private. This module is
// imported at boot (hooks.server.ts -> startInvalidationListener), and reading
// SvelteKit's dynamic-env proxy at module-eval time during server.init()
// deadlocks the handler import (unsettled top-level await -> exit 13). In
// adapter-node process.env carries the same values.
const SUB = {
  broadcaster: process.env.NATS_BROADCASTER_STATUS_SUBJECT ?? 'bagel.rpc.broadcaster.status.get',
  dashboard: process.env.NATS_DASHBOARD_SUBJECT_PREFIX ?? 'bagel.rpc.dashboard',
  commands: process.env.NATS_COMMANDS_SUBJECT_PREFIX ?? 'bagel.rpc.commands',
  projector: process.env.NATS_PROJECTOR_DASHBOARD_SUBJECT_PREFIX ?? 'bagel.rpc.projector.dashboard',
  outgress: process.env.NATS_OUTGRESS_SYSTEM_SUBJECT ?? 'twitch.outgress.system',
  audit: process.env.NATS_ADMIN_AUDIT_SUBJECT_PREFIX ?? 'bagel.rpc.admin.user.audit',
  delegation: process.env.NATS_DELEGATION_SUBJECT_PREFIX ?? 'bagel.rpc.delegation'
};

type CacheEntry<T> = { value?: T; promise?: Promise<T>; expires: number };

const cache = new Map<string, CacheEntry<unknown>>();

async function cached<T>(key: string, ttlMs: number, load: () => Promise<T>): Promise<T> {
  const now = Date.now();
  const hit = cache.get(key) as CacheEntry<T> | undefined;
  if (hit && hit.expires > now) {
    if (hit.value !== undefined) return hit.value;
    if (hit.promise) return hit.promise;
  }

  const promise = load();
  cache.set(key, { promise, expires: now + ttlMs });
  try {
    const value = await promise;
    cache.set(key, { value, expires: Date.now() + ttlMs });
    return value;
  } catch (err) {
    cache.delete(key);
    throw err;
  }
}

function invalidate(...prefixes: string[]) {
  for (const key of cache.keys()) {
    if (prefixes.some((prefix) => key.startsWith(prefix))) cache.delete(key);
  }
}

function invalidateUser(userId: string) {
  invalidate(`grant:${userId}`, `account:${userId}`, `commands:${userId}`, `modules:${userId}`, `delegations:${userId}`);
}

// TTLs are now a safety net backed by push-invalidation on the cache bus
// (bagel.cache.invalidate.<scope>). Raise them so a missed message still
// decays within a reasonable window rather than spamming RPC on every request.
const FAST_TTL_MS = 120_000;
const COMMAND_TTL_MS = 300_000;
const DELEGATION_TTL_MS = 300_000;
const BAN_TTL_MS = 120_000;

// Single-use dashboard delegation. Owners mint scoped links; invitees consume
// them once on login to gain a section-limited session over the owner's board.
export type DelegationGrant = {
  token: string;
  sections: string[];
  delegate_login: string;
  consumed: boolean;
};

export async function delegationCreate(
  ownerId: string,
  ownerLogin: string,
  sections: string[]
): Promise<string> {
  const r = await rpc<{ token?: string; error?: string }>(`${SUB.delegation}.create`, {
    owner_user_id: ownerId,
    owner_login: ownerLogin,
    sections
  });
  if (!r.token) throw new Error(r.error ?? 'create failed');
  invalidate(`delegations:${ownerId}`);
  return r.token;
}

export async function delegationGet(token: string): Promise<{
  owner_user_id: string;
  owner_login: string;
  sections: string[];
  consumed: boolean;
} | null> {
  return cached(`delegation-token:${token}`, FAST_TTL_MS, async () => {
    const r = await rpc<{
      owner_user_id?: string;
      owner_login?: string;
      sections?: string[];
      consumed?: boolean;
      error?: string;
    }>(`${SUB.delegation}.get`, { token }, READ_TIMEOUT_MS);
    if (r.error || !r.owner_user_id) return null;
    return {
      owner_user_id: r.owner_user_id,
      owner_login: r.owner_login ?? '',
      sections: r.sections ?? [],
      consumed: r.consumed === true
    };
  });
}

export async function delegationConsume(
  token: string,
  delegateId: string,
  delegateLogin: string
): Promise<{ ok: boolean; owner_user_id?: string; owner_login?: string; sections?: string[]; error?: string }> {
  const r = await rpc<{ ok: boolean; owner_user_id?: string; owner_login?: string; sections?: string[]; error?: string }>(`${SUB.delegation}.consume`, {
    token,
    delegate_user_id: delegateId,
    delegate_login: delegateLogin
  });
  invalidate(`delegation-token:${token}`, `delegations:${delegateId}`);
  if (r.owner_user_id) invalidate(`delegations:${r.owner_user_id}`);
  return r;
}

export async function delegationList(ownerId: string): Promise<DelegationGrant[]> {
  return cached(`delegations:${ownerId}:given`, DELEGATION_TTL_MS, async () => {
    const r = await rpc<{ grants?: DelegationGrant[] }>(
      `${SUB.delegation}.list`,
      { owner_user_id: ownerId },
      READ_TIMEOUT_MS
    );
    return r.grants ?? [];
  });
}

export async function delegationRevoke(ownerId: string, token: string): Promise<void> {
  const r = await rpc<{ ok?: boolean; error?: string }>(`${SUB.delegation}.revoke`, {
    owner_user_id: ownerId,
    token
  });
  if (!r.ok) throw new Error(r.error ?? 'revoke failed');
  invalidate(`delegation-token:${token}`, `delegations:${ownerId}`);
}

export async function delegationOptOut(delegateId: string, ownerId: string): Promise<void> {
  const r = await rpc<{ ok?: boolean; error?: string }>(`${SUB.delegation}.opt_out`, {
    delegate_user_id: delegateId,
    owner_user_id: ownerId
  });
  if (!r.ok) throw new Error(r.error ?? 'opt out failed');
  invalidate(`delegations:${delegateId}`, `delegations:${ownerId}`);
}

export async function delegationAccess(
  delegateId: string
): Promise<{ owner_user_id: string; owner_login: string; sections: string[] }[]> {
  return cached(`delegations:${delegateId}:access`, DELEGATION_TTL_MS, async () => {
    const r = await rpc<{
      grants?: { owner_user_id: string; owner_login: string; sections: string[] }[];
    }>(`${SUB.delegation}.access`, { delegate_user_id: delegateId }, READ_TIMEOUT_MS);
    return r.grants ?? [];
  });
}

// Enqueue an EventSub on/off job on the outgress system lane. Outgress runs the
// Helix calls under the shared rate-limit bucket: enabled=true (re)creates the
// channel's EventSub subscriptions, false deletes them.
export async function publishEventSub(broadcasterId: string, enabled: boolean): Promise<void> {
  await publish(SUB.outgress, {
    type: 'eventsub',
    broadcaster_id: broadcasterId,
    payload: { enabled }
  });
}

export async function tier(broadcasterId: string): Promise<Tier> {
  return cached(`tier:${broadcasterId}`, FAST_TTL_MS, async () => {
    const r = await rpc<{ tier: Tier }>(SUB.broadcaster, { broadcaster_id: broadcasterId }, 2000);
    return r.tier ?? 'standard';
  });
}

// isBanned reports whether the platform has banned the user (completes the
// admin "ban from service" action by blocking dashboard login). Routed through
// the shared cached() infra so concurrent cold reads coalesce on one promise
// instead of thundering-herding the RPC on a busy login burst.
// Fails OPEN: an RPC error deletes the key (cached() does this on throw) and
// returns false so a transient outage never locks everyone out.
export async function isBanned(userId: string): Promise<boolean> {
  try {
    return await cached(`ban:${userId}`, BAN_TTL_MS, async () => {
      const r = await rpc<{ banned?: boolean }>(SUB.broadcaster, { broadcaster_id: userId }, 2000);
      return r.banned === true;
    });
  } catch {
    return false;
  }
}

// auditImpersonation records a dashboard write performed while an admin is
// viewing as the user. Best-effort: a logging failure must never block the
// action it describes, so callers fire-and-forget and we swallow errors here.
export async function auditImpersonation(
  actorId: string,
  actorLogin: string,
  action: string,
  target: string,
  detail: string
): Promise<void> {
  try {
    await rpc(`${SUB.audit}.append`, {
      actor_id: actorId,
      actor_login: actorLogin,
      action,
      target,
      detail,
      ok: true,
      error: ''
    });
  } catch {
    /* best-effort */
  }
}

// Helper for dashboard actions taken during an admin "view as" session. Keeps
// target as the user dashboard and puts the exact action context in detail.
export function auditDashboardImpersonation(
  session: Session | null | undefined,
  action: string,
  detail = ''
): void {
  if (!session?.impersonator_id) return;
  auditImpersonation(
    session.impersonator_id,
    session.impersonator_login ?? '',
    `dashboard:${action}`,
    session.user_id,
    detail
  );
}

// Dashboard reads are cached primary-key lookups, so they return in low ms when
// healthy. Cap them at 2s (like tier()) so a slow or missing responder degrades
// the page fast instead of hanging SSR to the 5s default and tripping a gateway 500.
const READ_TIMEOUT_MS = 2000;

export async function hasGrant(userId: string): Promise<boolean> {
  return cached(`grant:${userId}`, FAST_TTL_MS, async () => {
    const r = await rpc<{ has_grant: boolean }>(
      `${SUB.dashboard}.grant_has`,
      { broadcaster_user_id: userId },
      READ_TIMEOUT_MS
    );
    return !!r.has_grant;
  });
}

export type AccountStatus = 'free' | 'paid' | 'vip';
export type AccountState = { active: boolean; status: AccountStatus };

function normalizeStatus(raw: string | undefined): AccountStatus {
  const s = (raw ?? 'free').toLowerCase();
  return s === 'paid' || s === 'vip' ? (s as AccountStatus) : 'free';
}

// Receive toggle and billing tier (free/paid/vip) in one round trip. The users
// service loads a single cached user view to answer both, so the page render
// asks once via state_get instead of separate active_get + status_get calls.
export async function accountState(userId: string): Promise<AccountState> {
  return cached(`account:${userId}`, FAST_TTL_MS, async () => {
    const r = await rpc<{ active: boolean; status: string }>(
      `${SUB.dashboard}.state_get`,
      { broadcaster_user_id: userId },
      READ_TIMEOUT_MS
    );
    return { active: !!r.active, status: normalizeStatus(r.status) };
  });
}

export async function setActive(userId: string, active: boolean): Promise<void> {
  await rpc(`${SUB.dashboard}.active_set`, { broadcaster_user_id: userId, active });
  invalidate(`account:${userId}`);
}

// Persist the broadcaster's Twitch OAuth grant (the per-channel bot token the
// dashboard consent mints). Called once on login: without it the user row exists
// but the bot has no token to act in the channel.
export async function saveGrant(
  userId: string,
  accessToken: string,
  refreshToken: string
): Promise<void> {
  await rpc(`${SUB.dashboard}.grant_save`, {
    broadcaster_user_id: userId,
    access_token: accessToken,
    refresh_token: refreshToken
  });
  invalidate(`grant:${userId}`, `account:${userId}`);
}

// Irreversibly delete the user's own account (and their owned delegations,
// cleared server-side). The caller drops the session cookie after this resolves.
export async function deleteSelf(userId: string): Promise<void> {
  const r = await rpc<{ ok?: boolean; error?: string }>(`${SUB.dashboard}.delete_self`, {
    user_id: userId
  });
  if (!r.ok) throw new Error(r.error ?? 'delete failed');
  invalidateUser(userId);
}

export async function listCommands(userId: string): Promise<CommandView[]> {
  return cached(`commands:${userId}`, COMMAND_TTL_MS, async () => {
    const r = await rpc<{ commands: CommandView[] }>(
      `${SUB.projector}.commands.get`,
      { user_id: userId },
      READ_TIMEOUT_MS
    );
    return r.commands ?? [];
  });
}

export interface ModuleView {
  name: string;
  is_enabled: boolean;
  configs?: unknown;
}

export async function listModules(userId: string): Promise<ModuleView[]> {
  return cached(`modules:${userId}`, COMMAND_TTL_MS, async () => {
    const r = await rpc<{ modules: ModuleView[] }>(
      `${SUB.projector}.modules.get`,
      { user_id: userId },
      READ_TIMEOUT_MS
    );
    return r.modules ?? [];
  });
}

async function replaceProjectedCommands(userId: string, commands: CommandView[]): Promise<void> {
  try {
    await rpc(
      `${SUB.projector}.commands.replace`,
      { user_id: userId, commands },
      750
    );
  } catch {
    /* best-effort: command change events reconcile Valkey shortly after */
  }
}

export async function replaceProjectedModules(userId: string, modules: ModuleView[]): Promise<void> {
  try {
    await rpc(
      `${SUB.projector}.modules.replace`,
      { user_id: userId, modules },
      750
    );
  } catch {
    /* best-effort: module change events reconcile Valkey shortly after */
  }
}

export interface CommandInput {
  name: string;
  aliases: string[];
  response: string;
  isActive: boolean;
  streamOnlineOnly: boolean;
  perm: Perm;
  cooldown: number;
  allowedUserId: string;
}

// originalName, when set and different from cmd.name, renames the command: the
// commands service updates the existing row's name field in place instead of
// deleting the old command and recreating it under the new name.
export async function upsertCommand(
  userId: string,
  cmd: CommandInput,
  originalName?: string
): Promise<{ commands: CommandView[]; error?: string }> {
  const r = await rpc<{ commands: CommandView[]; error?: string }>(`${SUB.commands}.upsert`, {
    user_id: userId,
    name: cmd.name,
    aliases: cmd.aliases,
    response: cmd.response,
    is_active: cmd.isActive,
    stream_online_only: cmd.streamOnlineOnly,
    perm: cmd.perm,
    cooldown: cmd.cooldown,
    allowed_user_id: cmd.allowedUserId,
    original_name: originalName ?? ''
  });
  if (!r.error) {
    const commands = r.commands ?? [];
    await replaceProjectedCommands(userId, commands);
    cache.set(`commands:${userId}`, { value: commands, expires: Date.now() + COMMAND_TTL_MS });
  } else invalidate(`commands:${userId}`);
  return { commands: r.commands ?? [], error: r.error };
}

export async function deleteCommand(
  userId: string,
  name: string
): Promise<{ commands: CommandView[]; error?: string }> {
  const r = await rpc<{ commands: CommandView[]; error?: string }>(`${SUB.commands}.delete`, {
    user_id: userId,
    name
  });
  if (!r.error) {
    const commands = r.commands ?? [];
    await replaceProjectedCommands(userId, commands);
    cache.set(`commands:${userId}`, { value: commands, expires: Date.now() + COMMAND_TTL_MS });
  } else invalidate(`commands:${userId}`);
  return { commands: r.commands ?? [], error: r.error };
}

// Scope -> cache key routing for the invalidation bus.
// grant   -> grant:<id>, account:<id>
// status  -> account:<id>, ban:<id>
// commands -> commands:<id>
// modules  -> modules:<id>
// delegation -> delegations:<id> (prefix covers :given and :access)
// <missing/empty> -> full invalidateUser(id) + ban:<id>
function applyInvalidation(broadcasterId: string, scope: string | undefined): void {
  switch (scope) {
    case 'grant':
      invalidate(`grant:${broadcasterId}`, `account:${broadcasterId}`);
      break;
    case 'status':
      invalidate(`account:${broadcasterId}`, `ban:${broadcasterId}`);
      break;
    case 'commands':
      invalidate(`commands:${broadcasterId}`);
      break;
    case 'modules':
      invalidate(`modules:${broadcasterId}`);
      break;
    case 'delegation':
      invalidate(`delegations:${broadcasterId}`);
      break;
    default:
      // No scope or unknown scope: drop everything for this user (back-compat).
      invalidateUser(broadcasterId);
      invalidate(`ban:${broadcasterId}`);
  }
}

/**
 * Subscribe to the cache-invalidation bus so writes in other services (Go)
 * push-drop the affected keys without waiting on TTL expiry. Call once at
 * server boot (hooks.server.ts). Fire-and-forget; resilient to NATS restarts
 * via the shared subscribe() primitive.
 */
export function startInvalidationListener(): void {
  const prefix = process.env.NATS_CACHE_INVALIDATION_PREFIX ?? 'bagel.cache.invalidate';

  subscribe(prefix + '.>', (subject, data) => {
    try {
      const msg = JSON.parse(new TextDecoder().decode(data)) as {
        broadcaster_id?: unknown;
      };
      const id = typeof msg.broadcaster_id === 'string' ? msg.broadcaster_id : undefined;
      if (!id) return;
      // Scope is the last dot-segment of the message subject
      // e.g. "bagel.cache.invalidate.status" -> "status"
      const scope = subject.slice(subject.lastIndexOf('.') + 1) || undefined;
      applyInvalidation(id, scope);
    } catch {
      // Malformed message — ignore.
    }
  });
}
