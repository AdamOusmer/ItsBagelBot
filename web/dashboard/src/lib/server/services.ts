// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

import newrelic from 'newrelic';
import { rpc, publish } from '@bagel/kit/server/nats';
import { createCacheFabric } from '@bagel/kit/server/cache-fabric';
import { POLICY, type CachePolicy } from '@bagel/kit/server/cache-keys';
import { defineRead, defineWrite, READ_TIMEOUT_MS } from '@bagel/kit/server/service';
import type { ScopeMap } from '@bagel/kit/server/invalidation';
import * as valkey from '@bagel/kit/server/valkey-store';
import type { Tier } from '@bagel/kit';
import type { Session } from './session';
import * as liveHub from './live-hub';
import { dashboardL1CacheCapacity } from './config-sanity';

// process.env, not $env/dynamic/private: the dynamic-env proxy deadlocks server.init() at boot.
// `||`, not `??`: a set-but-blank env var would build subjects no responder answers.
export const SUB = {
  broadcaster: process.env.NATS_BROADCASTER_STATUS_SUBJECT || 'bagel.rpc.broadcaster.status.get',
  dashboard: process.env.NATS_DASHBOARD_SUBJECT_PREFIX || 'bagel.rpc.dashboard',
  commands: process.env.NATS_COMMANDS_SUBJECT_PREFIX || 'bagel.rpc.commands',
  modules: process.env.NATS_MODULES_SUBJECT_PREFIX || 'bagel.rpc.modules',
  projector: process.env.NATS_PROJECTOR_DASHBOARD_SUBJECT_PREFIX || 'bagel.rpc.projector.dashboard',
  outgress: process.env.NATS_OUTGRESS_SYSTEM_SUBJECT || 'twitch.outgress.system',
  outgressRpc: process.env.NATS_OUTGRESS_RPC_PREFIX || 'bagel.rpc.outgress',
  dingressRpc: process.env.NATS_DINGRESS_RPC_PREFIX || 'bagel.rpc.dingress',
  gossip: process.env.NATS_GOSSIP_SUBJECT_PREFIX || 'bagel.rpc.gossip',
  loyalty: process.env.NATS_LOYALTY_SUBJECT_PREFIX || 'bagel.rpc.loyalty',
  goveeKey: process.env.NATS_MODULES_GOVEE_SUBJECT_PREFIX || 'bagel.rpc.modules.govee',
  spotifyKey: process.env.NATS_MODULES_SPOTIFY_SUBJECT_PREFIX || 'bagel.rpc.modules.spotify',
  audit: process.env.NATS_ADMIN_AUDIT_SUBJECT_PREFIX || 'bagel.rpc.admin.user.audit',
  delegation: process.env.NATS_DELEGATION_SUBJECT_PREFIX || 'bagel.rpc.delegation',
  notifications: process.env.NATS_NOTIFICATIONS_SUBJECT_PREFIX || 'bagel.rpc.notifications',
  transactions: process.env.NATS_TRANSACTIONS_SUBJECT_PREFIX || 'bagel.rpc.transactions'
};

export function userPrefixes(id: string): string[] {
  return [`grant:${id}`, `account:${id}`, `tier:${id}`, `billing-state:${id}`, `commands:${id}`, `modules:${id}`, `delegations:${id}`, `locale:${id}`, `cursor:${id}`, `govee-devices:${id}`, `commands_page:${id}`];
}

export const SCOPES: ScopeMap = {
  grant: (id) => [`grant:${id}`, `account:${id}`],
  status: (id) => [`account:${id}`, `tier:${id}`, `ban:${id}`, `billing-state:${id}`],
  commands: (id) => [`commands:${id}`],
  modules: (id) => [`modules:${id}`],
  delegation: (id) => [`delegations:${id}`],
  notifications: (id) => [`notifications:${id}`, 'notifications:all'],
  locale: (id) => [`locale:${id}`],
  cursor: (id) => [`cursor:${id}`],
  commands_page: (id) => [`commands_page:${id}`],
  '*': (id) => [...userPrefixes(id), `ban:${id}`]
};

export const fabric = createCacheFabric({
  app: 'dashboard',
  scopes: SCOPES,
  capacity: dashboardL1CacheCapacity(process.env),
  onInvalidation: (scope, id) => liveHub.publish(id, scope)
});

function cached<T>(key: string, policy: CachePolicy, load: () => Promise<T>): Promise<T> {
  return fabric.readKey(key, policy, load);
}

export function invalidate(...prefixes: string[]) {
  fabric.invalidate(...prefixes);
}

function invalidateUser(userId: string) {
  invalidate(...userPrefixes(userId));
}

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

export const delegationGet = defineRead({
  subject: `${SUB.delegation}.get`,
  request: (token: string) => ({ token }),
  map: (r: {
    owner_user_id?: string;
    owner_login?: string;
    sections?: string[];
    consumed?: boolean;
    error?: string;
  }): {
    owner_user_id: string;
    owner_login: string;
    sections: string[];
    consumed: boolean;
  } | null => {
    if (r.error || !r.owner_user_id) return null;
    return {
      owner_user_id: r.owner_user_id,
      owner_login: r.owner_login ?? '',
      sections: r.sections ?? [],
      consumed: r.consumed === true
    };
  },
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (token: string) => `delegation-token:${token}`,
    policy: POLICY.entity
  }
});

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

export const delegationList = defineRead({
  subject: `${SUB.delegation}.list`,
  request: (ownerId: string) => ({ owner_user_id: ownerId }),
  map: (r: { grants?: DelegationGrant[] }) => r.grants ?? [],
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (ownerId: string) => `delegations:${ownerId}:given`,
    policy: POLICY.entity
  }
});

export async function delegationUpdate(ownerId: string, token: string, sections: string[]): Promise<void> {
  const r = await rpc<{ ok?: boolean; error?: string; delegate_user_id?: string }>(`${SUB.delegation}.update`, {
    owner_user_id: ownerId,
    token,
    sections
  });
  if (!r.ok) throw new Error(r.error ?? 'update failed');
  if (r.delegate_user_id) {
    invalidate(`delegation-token:${token}`, `delegations:${ownerId}`, `delegations:${r.delegate_user_id}`);
  } else {
    invalidate(`delegation-token:${token}`, `delegations:${ownerId}`);
  }
}

export async function delegationRevoke(ownerId: string, token: string): Promise<void> {
  const r = await rpc<{ ok?: boolean; error?: string; delegate_user_id?: string }>(`${SUB.delegation}.revoke`, {
    owner_user_id: ownerId,
    token
  });
  if (!r.ok) throw new Error(r.error ?? 'revoke failed');
  if (r.delegate_user_id) {
    invalidate(`delegation-token:${token}`, `delegations:${ownerId}`, `delegations:${r.delegate_user_id}`);
  } else {
    invalidate(`delegation-token:${token}`, `delegations:${ownerId}`);
  }
}

export async function delegationOptOut(delegateId: string, ownerId: string): Promise<void> {
  const r = await rpc<{ ok?: boolean; error?: string }>(`${SUB.delegation}.opt_out`, {
    delegate_user_id: delegateId,
    owner_user_id: ownerId
  });
  if (!r.ok) throw new Error(r.error ?? 'opt out failed');
  invalidate(`delegations:${delegateId}`, `delegations:${ownerId}`);
}

export const delegationAccess = defineRead({
  subject: `${SUB.delegation}.access`,
  request: (delegateId: string) => ({ delegate_user_id: delegateId }),
  map: (r: {
    grants?: { owner_user_id: string; owner_login: string; sections: string[] }[];
  }) => r.grants ?? [],
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (delegateId: string) => `delegations:${delegateId}:access`,
    policy: POLICY.entity
  }
});

export async function publishEventSub(broadcasterId: string, enabled: boolean): Promise<void> {
  await publish(SUB.outgress, {
    type: 'eventsub',
    broadcaster_id: broadcasterId,
    payload: { enabled }
  });
}

export async function publishEventSubReconnect(broadcasterId: string): Promise<void> {
  await publish(SUB.outgress, {
    type: 'eventsub',
    broadcaster_id: broadcasterId,
    payload: { mode: 'reconnect' }
  });
}

export async function publishEventSubEnsureOptional(broadcasterId: string): Promise<void> {
  await publish(SUB.outgress, {
    type: 'eventsub',
    broadcaster_id: broadcasterId,
    payload: { mode: 'ensure_optional' }
  });
}

export type ChannelSubState = {
  state: 'ok' | 'pending' | 'failing' | 'revoked' | 'chat_banned' | 'unenrolled' | 'unknown';
  error: string;
  checkedAt: string | null;
};

function unknownSubState(): ChannelSubState {
  return { state: 'unknown', error: '', checkedAt: null };
}

// Keep 'revoked' and 'chat_banned' known: unknown reads as 'unenrolled' and triggers futile enables.
const KNOWN_SUB_STATES = ['ok', 'pending', 'failing', 'revoked', 'chat_banned'] as const;

function isKnownSubState(s: string): s is (typeof KNOWN_SUB_STATES)[number] {
  return (KNOWN_SUB_STATES as readonly string[]).includes(s);
}

export async function channelSubState(broadcasterId: string): Promise<ChannelSubState> {
  try {
    const r = await rpc<{
      found: boolean;
      channel?: { sub_state: string; sub_error: string; sub_checked_at: string };
    }>(`${SUB.outgressRpc}.channel.get`, { broadcaster_id: broadcasterId }, 2000);
    const c = r.found ? r.channel : undefined;
    if (!c || !isKnownSubState(c.sub_state)) {
      return { state: 'unenrolled', error: '', checkedAt: null };
    }
    return {
      state: c.sub_state,
      error: c.sub_error || '',
      checkedAt: c.sub_checked_at || null
    };
  } catch {
    return unknownSubState();
  }
}

// Must match the projector's tierFromStatus.
function tierFromStatus(status: string): Tier {
  const s = status.toLowerCase();
  return s === 'premium' || s === 'vip' || s === 'paid' ? 'premium' : 'standard';
}

export const tier = defineRead({
  subject: SUB.broadcaster,
  request: (broadcasterId: string) => ({ broadcaster_id: broadcasterId }),
  map: (r: { tier: Tier }) => r.tier ?? 'standard',
  timeoutMs: 2000,
  cache: {
    fabric,
    key: (broadcasterId: string) => `tier:${broadcasterId}`,
    policy: POLICY.entity,
    l2: async (broadcasterId: string) => {
      const u = await valkey.getUser(broadcasterId);
      if (!u.known) return { hit: false, value: 'standard' as Tier };
      return { hit: true, value: u.active ? tierFromStatus(u.status) : 'standard' };
    }
  }
});

export async function isBanned(userId: string): Promise<boolean> {
  try {
    return await cached(`ban:${userId}`, POLICY.security, async () => {
      const u = await valkey.getUser(userId);
      if (u.known) return u.banned;
      const r = await rpc<{ banned?: boolean }>(SUB.broadcaster, { broadcaster_id: userId }, 2000);
      return r.banned === true;
    });
  } catch (err) {
    newrelic.noticeError(err instanceof Error ? err : new Error(String(err)), {
      component: 'ban-check',
      userId
    });
    return false;
  }
}

export type AuditEntry = {
  actorId: string;
  actorLogin: string;
  action: string;
  target: string;
  detail: string;
};

export async function auditImpersonation(entry: AuditEntry): Promise<void> {
  try {
    await rpc(`${SUB.audit}.append`, {
      actor_id: entry.actorId,
      actor_login: entry.actorLogin,
      action: entry.action,
      target: entry.target,
      detail: entry.detail,
      ok: true,
      error: ''
    });
  } catch {}
}

export function auditDashboardImpersonation(
  session: Session | null | undefined,
  action: string,
  detail = ''
): void {
  if (!session?.impersonator_id) return;
  auditImpersonation({
    actorId: session.impersonator_id,
    actorLogin: session.impersonator_login ?? '',
    action: `dashboard:${action}`,
    target: session.user_id,
    detail
  });
}

export const hasGrant = defineRead({
  subject: `${SUB.dashboard}.grant_has`,
  request: (userId: string) => ({ broadcaster_user_id: userId }),
  map: (r: { has_grant: boolean }) => !!r.has_grant,
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (userId: string) => `grant:${userId}`,
    policy: POLICY.entity
  }
});

export type AccountStatus = 'free' | 'paid' | 'vip';
export type AccountState = { active: boolean; status: AccountStatus; onboarded: boolean; creatorCode: string | null; username: string; displayName: string };

function normalizeStatus(raw: string | undefined): AccountStatus {
  const s = (raw ?? 'free').toLowerCase();
  return s === 'paid' || s === 'vip' ? (s as AccountStatus) : 'free';
}

export const accountState = defineRead({
  subject: `${SUB.dashboard}.state_get`,
  request: (userId: string) => ({ broadcaster_user_id: userId }),
  map: (r: {
    active: boolean;
    status: string;
    onboarded?: boolean;
    creator_code?: string | null;
    username?: string | null;
    display_name?: string | null;
  }): AccountState => ({
    active: !!r.active,
    status: normalizeStatus(r.status),
    onboarded: !!r.onboarded,
    creatorCode: r.creator_code?.trim() ? r.creator_code : null,
    username: (r.username ?? '').trim(),
    displayName: (r.display_name ?? '').trim()
  }),
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (userId: string) => `account:${userId}`,
    policy: POLICY.entity,
    l2: async (userId: string) => {
      const u = await valkey.getUser(userId);
      if (!u.known) return { hit: false, value: { active: false, status: 'free' as AccountStatus, onboarded: false, creatorCode: null, username: '', displayName: '' } };
      return { hit: false, value: { active: u.active, status: normalizeStatus(u.status), onboarded: false, creatorCode: null, username: '', displayName: '' } };
    }
  }
});

export const setActive = defineWrite({
  subject: `${SUB.dashboard}.active_set`,
  request: (userId: string, active: boolean) => ({ broadcaster_user_id: userId, active }),
  after: (_result: unknown, userId: string) => invalidate(`account:${userId}`)
});

export const setOnboarded = defineWrite({
  subject: `${SUB.dashboard}.onboarded_set`,
  request: (userId: string, onboarded: boolean) => ({ broadcaster_user_id: userId, onboarded }),
  after: (_result: unknown, userId: string) => invalidate(`account:${userId}`)
});

function prefRead<T>(scope: string, map: (r: Record<string, unknown>) => T) {
  return defineRead<[string], Record<string, unknown>, T>({
    subject: `${SUB.dashboard}.state_get`,
    request: (userId: string) => ({ broadcaster_user_id: userId }),
    map,
    timeoutMs: READ_TIMEOUT_MS,
    cache: { fabric, key: (userId: string) => `${scope}:${userId}`, policy: POLICY.entity }
  });
}

function prefWrite<V>(verb: string, field: string, scope: string) {
  return defineWrite<[string, V], unknown>({
    subject: `${SUB.dashboard}.${verb}`,
    request: (userId: string, value: V) => ({ broadcaster_user_id: userId, [field]: value }),
    after: (_result: unknown, userId: string) => invalidate(`${scope}:${userId}`)
  });
}

export const userLocale = prefRead('locale', (r) => (typeof r.locale === 'string' && r.locale ? r.locale : 'en'));
export const setLocale = prefWrite<string>('locale_set', 'locale', 'locale');

export const userCursor = prefRead('cursor', (r) => r.custom_cursor !== false);
export const setCursor = prefWrite<boolean>('cursor_set', 'custom_cursor', 'cursor');

export const userCommandsPage = prefRead('commands_page', (r) => r.commands_page_hidden !== true);
export const setCommandsPage = prefWrite<boolean>('commands_page_set', 'commands_page_hidden', 'commands_page');

export const saveGrant = defineWrite({
  subject: `${SUB.dashboard}.grant_save`,
  request: (userId: string, accessToken: string, refreshToken: string) => ({
    broadcaster_user_id: userId,
    access_token: accessToken,
    refresh_token: refreshToken
  }),
  after: (_result: unknown, userId: string) => invalidate(`grant:${userId}`, `account:${userId}`)
});

export type BillingGrantSource = 'tebex' | 'admin' | '';

export type BillingState = {
  active: boolean;
  status: AccountStatus;
  expiresAt: string | null;
  source: BillingGrantSource;
  subscriptionRef: string | null;
  cancelPending: boolean;
};

export type ResolvedChannel = { userId: string; username: string; displayName: string };

export const resolveLogin = defineRead({
  subject: `${SUB.dashboard}.login_resolve`,
  request: (login: string) => ({ login }),
  map: (r: { user_id?: string; username?: string; display_name?: string }): ResolvedChannel => ({
    userId: (r.user_id ?? '').trim(),
    username: (r.username ?? '').trim(),
    displayName: (r.display_name ?? '').trim()
  }),
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (login: string) => `login:${login}`,
    policy: POLICY.entity
  }
});

// No Valkey L2: an L2 hit on the projected user hash would drop the paid-until date.
export const billingState = defineRead({
  subject: `${SUB.dashboard}.state_get`,
  request: (userId: string) => ({ broadcaster_user_id: userId }),
  map: (r: {
    active: boolean;
    status: string;
    expires_at?: string;
    source?: string;
    subscription_ref?: string;
    subscription_cancel_pending?: boolean;
  }): BillingState => ({
    active: !!r.active,
    status: normalizeStatus(r.status),
    expiresAt: r.expires_at ?? null,
    source: (r.source as BillingGrantSource) ?? '',
    subscriptionRef: r.subscription_ref ?? null,
    cancelPending: !!r.subscription_cancel_pending
  }),
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (userId: string) => `billing-state:${userId}`,
    policy: POLICY.entity
  }
});

export type CheckoutBasket = { ident: string; checkoutUrl: string | null; recipientLogin: string | null };

export type CheckoutPackageType = 'single' | 'subscription';

export type CheckoutRequest = {
  userId: string;
  username: string;
  recipientUsername?: string;
  ipAddress?: string;
  packageType?: CheckoutPackageType;
  giftMessage?: string;
};

export async function checkoutBasketCreate(req: CheckoutRequest): Promise<CheckoutBasket> {
  const r = await rpc<{ ident?: string; checkout_url?: string; recipient_login?: string }>(
    `${SUB.transactions}.basket_create`,
    {
      user_id: req.userId,
      username: req.username,
      recipient_username: req.recipientUsername || undefined,
      ip_address: req.ipAddress || undefined,
      package_type: req.packageType || undefined,
      gift_message: req.giftMessage || undefined
    },
    16000
  );
  if (!r.ident) throw new Error('basket create returned no ident');
  return { ident: r.ident, checkoutUrl: r.checkout_url ?? null, recipientLogin: r.recipient_login ?? null };
}

export type NotificationWire = {
  id: number;
  scope: 'broadcast' | 'direct';
  title: string;
  body: string;
  level: 'info' | 'success' | 'warning' | 'critical';
  created_by_login: string;
  created_at: string;
  expires_at?: string;
  read: boolean;
};

export type NotificationsForUser = {
  notifications: NotificationWire[];
  unreadCount: number;
};

export const notificationsForUser = defineRead({
  subject: `${SUB.notifications}.list`,
  request: (userId: string) => ({ user_id: userId }),
  map: (r: { notifications?: NotificationWire[]; unread_count?: number }): NotificationsForUser => ({
    notifications: r.notifications ?? [],
    unreadCount: r.unread_count ?? 0
  }),
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (userId: string) => `notifications:${userId}`,
    policy: POLICY.live
  }
});

export const notificationMarkRead = defineWrite({
  subject: `${SUB.notifications}.mark_read`,
  request: (userId: string, notificationId: number) => ({
    user_id: userId,
    notification_id: String(notificationId)
  }),
  after: (_result: unknown, userId: string) => invalidate(`notifications:${userId}`)
});

export const notificationMarkPeeked = defineWrite({
  subject: `${SUB.notifications}.mark_peeked`,
  request: (userId: string) => ({ user_id: userId }),
  after: (_result: unknown, userId: string) => invalidate(`notifications:${userId}`)
});

/** Irreversible; the caller must drop the session cookie after it resolves. */
export async function deleteSelf(userId: string): Promise<void> {
  const r = await rpc<{ ok?: boolean; error?: string }>(`${SUB.dashboard}.delete_self`, {
    user_id: userId
  });
  if (!r.ok) throw new Error(r.error ?? 'delete failed');
  invalidateUser(userId);
}

export function startInvalidationListener(): void {
  fabric.start();
}
