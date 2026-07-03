// Dashboard-facing RPC wrappers over the shared NATS client. Subjects come from
// env with the same defaults as the retired Go dashboard tier.
import newrelic from 'newrelic';
import { rpc, publish } from '@bagel/shared/server/nats';
import { createCacheFabric } from '@bagel/shared/server/cache-fabric';
import { POLICY, type CachePolicy } from '@bagel/shared/server/cache-keys';
import { defineRead, defineWrite, READ_TIMEOUT_MS } from '@bagel/shared/server/service';
import type { ScopeMap } from '@bagel/shared/server/invalidation';
import * as valkey from '@bagel/shared/server/valkey-store';
import type { Tier } from '@bagel/shared';
import type { Session } from './session';

// Subjects come from process.env, NOT $env/dynamic/private. This module is
// imported at boot (hooks.server.ts -> startInvalidationListener), and reading
// SvelteKit's dynamic-env proxy at module-eval time during server.init()
// deadlocks the handler import (unsettled top-level await -> exit 13). In
// adapter-node process.env carries the same values.
export const SUB = {
  broadcaster: process.env.NATS_BROADCASTER_STATUS_SUBJECT ?? 'bagel.rpc.broadcaster.status.get',
  dashboard: process.env.NATS_DASHBOARD_SUBJECT_PREFIX ?? 'bagel.rpc.dashboard',
  commands: process.env.NATS_COMMANDS_SUBJECT_PREFIX ?? 'bagel.rpc.commands',
  modules: process.env.NATS_MODULES_SUBJECT_PREFIX ?? 'bagel.rpc.modules',
  projector: process.env.NATS_PROJECTOR_DASHBOARD_SUBJECT_PREFIX ?? 'bagel.rpc.projector.dashboard',
  outgress: process.env.NATS_OUTGRESS_SYSTEM_SUBJECT ?? 'twitch.outgress.system',
  outgressRpc: process.env.NATS_OUTGRESS_RPC_PREFIX ?? 'bagel.rpc.outgress',
  audit: process.env.NATS_ADMIN_AUDIT_SUBJECT_PREFIX ?? 'bagel.rpc.admin.user.audit',
  delegation: process.env.NATS_DELEGATION_SUBJECT_PREFIX ?? 'bagel.rpc.delegation',
  notifications: process.env.NATS_NOTIFICATIONS_SUBJECT_PREFIX ?? 'bagel.rpc.notifications',
  transactions: process.env.NATS_TRANSACTIONS_SUBJECT_PREFIX ?? 'bagel.rpc.transactions'
};

function userPrefixes(id: string): string[] {
  return [`grant:${id}`, `account:${id}`, `tier:${id}`, `billing-state:${id}`, `commands:${id}`, `modules:${id}`, `delegations:${id}`, `locale:${id}`];
}

// Scope -> cache key routing for the invalidation bus, declared as data. The
// shared router picks the scope from the subject's last segment; unknown or
// missing scopes fall through to '*' (coarse per-user flush, back-compat).
const SCOPES: ScopeMap = {
  grant: (id) => [`grant:${id}`, `account:${id}`],
  // Billing webhooks land as status invalidations after entitlement changes.
  status: (id) => [`account:${id}`, `tier:${id}`, `ban:${id}`, `billing-state:${id}`],
  commands: (id) => [`commands:${id}`],
  modules: (id) => [`modules:${id}`],
  delegation: (id) => [`delegations:${id}`],
  notifications: (id) => [`notifications:${id}`, 'notifications:all'],
  locale: (id) => [`locale:${id}`],
  '*': (id) => [...userPrefixes(id), `ban:${id}`]
};

// Hybrid read path: L1 SwrCache (+ push invalidation, SWR, stale-if-error) over
// per-key Valkey readers over RPC. Freshness policy per data class lives in the
// shared POLICY table; the bus, not the clock, is the main freshness lever.
export const fabric = createCacheFabric({ app: 'dashboard', scopes: SCOPES });

function cached<T>(key: string, policy: CachePolicy, load: () => Promise<T>): Promise<T> {
  return fabric.readKey(key, policy, load);
}

export function invalidate(...prefixes: string[]) {
  fabric.invalidate(...prefixes);
}

function invalidateUser(userId: string) {
  invalidate(...userPrefixes(userId));
}

// Single-use dashboard delegation. Owners mint scoped links; invitees consume
// them once on login to gain a section-limited session over the owner's board.
export type DelegationGrant = {
  token: string;
  sections: string[];
  delegate_login: string;
  consumed: boolean;
};

// Custom error mapping (create failed / revoke failed / opt out failed) doesn't
// fit defineWrite's identity/map result shape cleanly, so this stays hand-written.
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

// Enqueue an atomic reconnect job: outgress drops all existing subs and
// recreates them in a single-flight, all-or-nothing operation.
export async function publishEventSubReconnect(broadcasterId: string): Promise<void> {
  await publish(SUB.outgress, {
    type: 'eventsub',
    broadcaster_id: broadcasterId,
    payload: { mode: 'reconnect' }
  });
}

export type ChannelSubState = {
  state: 'ok' | 'pending' | 'failing' | 'unknown';
  error: string;
  checkedAt: string | null;
};

// Read the persisted EventSub enroll state for a channel. Fails safe: returns
// 'unknown' on RPC error so a transient outage never blocks page render. The
// fail-open catch doesn't fit defineRead cleanly (no cache involved either), so
// this stays hand-written.
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

// Resolve a Tier from the raw billing status, mirroring the projector's
// tierFromStatus rule so the Valkey-served tier agrees with the RPC one.
function tierFromStatus(status: string): Tier {
  const s = status.toLowerCase();
  return s === 'premium' || s === 'vip' || s === 'paid' ? 'premium' : 'standard';
}

// 3-tier read: tier-1 LRU (cached) -> tier-2 Valkey settings hash -> tier-3
// projector/broadcaster RPC on a cold key. The Valkey miss path is transparent
// (getUser returns known:false on any failure) so a Valkey outage just falls to
// RPC, never breaking SSR.
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

// isBanned reports whether the platform has banned the user (completes the
// admin "ban from service" action by blocking dashboard login). Routed through
// the shared cached() infra so concurrent cold reads coalesce on one promise
// instead of thundering-herding the RPC on a busy login burst.
// Outage posture: the `security` policy serves the last KNOWN state on loader
// error (stale-if-error window), so an already-banned user stays banned through
// a users-service outage. Only users with no cached state at all fail OPEN
// (return false) — fail-closed would lock every user out during any outage.
// The fail-open catch around the whole cached() call doesn't fit defineRead
// (which has no outer error handling), so this stays hand-written.
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
export type AccountState = { active: boolean; status: AccountStatus };

function normalizeStatus(raw: string | undefined): AccountStatus {
  const s = (raw ?? 'free').toLowerCase();
  return s === 'paid' || s === 'vip' ? (s as AccountStatus) : 'free';
}

// Receive toggle and billing tier (free/paid/vip) in one round trip. The users
// service loads a single cached user view to answer both, so the page render
// asks once via state_get instead of separate active_get + status_get calls.
export const accountState = defineRead({
  subject: `${SUB.dashboard}.state_get`,
  request: (userId: string) => ({ broadcaster_user_id: userId }),
  map: (r: { active: boolean; status: string }): AccountState => ({
    active: !!r.active,
    status: normalizeStatus(r.status)
  }),
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (userId: string) => `account:${userId}`,
    policy: POLICY.entity,
    l2: async (userId: string) => {
      const u = await valkey.getUser(userId);
      if (!u.known) return { hit: false, value: { active: false, status: 'free' as AccountStatus } };
      return { hit: true, value: { active: u.active, status: normalizeStatus(u.status) } };
    }
  }
});

export const setActive = defineWrite({
  subject: `${SUB.dashboard}.active_set`,
  request: (userId: string, active: boolean) => ({ broadcaster_user_id: userId, active }),
  after: (_result: unknown, userId: string) => invalidate(`account:${userId}`)
});

// Persisted console UI language. state_get carries it back; own cache key with
// no Valkey L2 (the projected user hash has no locale), and it is only read at
// login to seed the preference cookie, so a little staleness is harmless.
export const userLocale = defineRead({
  subject: `${SUB.dashboard}.state_get`,
  request: (userId: string) => ({ broadcaster_user_id: userId }),
  map: (r: { locale?: string }): string => r.locale || 'en',
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (userId: string) => `locale:${userId}`,
    policy: POLICY.entity
  }
});

// Write the user's language choice through to the users service. The switcher
// also sets the cookie so the current render flips immediately; this is what
// makes the choice follow the account to another browser/device.
export const setLocale = defineWrite({
  subject: `${SUB.dashboard}.locale_set`,
  request: (userId: string, locale: string) => ({ broadcaster_user_id: userId, locale }),
  after: (_result: unknown, userId: string) => invalidate(`locale:${userId}`)
});

// Persist the broadcaster's Twitch OAuth grant (the per-channel bot token the
// dashboard consent mints). Called once on login: without it the user row exists
// but the bot has no token to act in the channel.
export const saveGrant = defineWrite({
  subject: `${SUB.dashboard}.grant_save`,
  request: (userId: string, accessToken: string, refreshToken: string) => ({
    broadcaster_user_id: userId,
    access_token: accessToken,
    refresh_token: refreshToken
  }),
  after: (_result: unknown, userId: string) => invalidate(`grant:${userId}`, `account:${userId}`)
});

// ---------------------------------------------------------------------------
// Billing (local entitlement status; checkout/account management live on Tebex)

export type BillingState = {
  active: boolean;
  status: AccountStatus;
  // End of the current paid period (Tebex or staff grant); absent for free/vip.
  expiresAt: string | null;
  // 'tebex' | 'admin' | '' — who granted the paid period.
  source: string;
  subscriptionRef: string | null;
  cancelPending: boolean;
};

// Billing-page variant of accountState: same state_get RPC but carrying the
// paid-until date and grant source. Deliberately no Valkey L2 (the projected
// user hash has no expiry), so a cache hit can never silently drop the date.
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
    source: r.source ?? '',
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

// Mint a Tebex Headless basket via the transactions service. The checkout URL
// is always Tebex-hosted; the dashboard redirects the browser there instead of
// embedding payment UI. When recipientUsername is set the basket is a gift: the
// transactions service resolves and vets the recipient (registered, not banned,
// not already premium) and the entitlement lands on them while this user pays.
// Never cached — every checkout attempt gets a fresh basket. Basket creation is
// two Tebex HTTP calls upstream, so the timeout is looser than the in-cluster
// read budget.
export type CheckoutBasket = { ident: string; checkoutUrl: string | null; recipientLogin: string | null };

export type CheckoutPackageType = 'single' | 'subscription';

export async function checkoutBasketCreate(
  userId: string,
  username: string,
  recipientUsername?: string,
  ipAddress?: string,
  packageType?: CheckoutPackageType,
  giftMessage?: string
): Promise<CheckoutBasket> {
  const r = await rpc<{ ident?: string; checkout_url?: string; recipient_login?: string }>(
    `${SUB.transactions}.basket_create`,
    {
      user_id: userId,
      username,
      recipient_username: recipientUsername || undefined,
      ip_address: ipAddress || undefined,
      package_type: packageType || undefined,
      gift_message: giftMessage || undefined
    },
    16000
  );
  if (!r.ident) throw new Error('basket create returned no ident');
  return { ident: r.ident, checkoutUrl: r.checkout_url ?? null, recipientLogin: r.recipient_login ?? null };
}

// ---------------------------------------------------------------------------
// Notifications (notifications service)

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

// Broadcast sends can't be push-invalidated per user (the sender doesn't know
// every recipient's cache key), so this rides a short freshness window
// instead of relying solely on the invalidation bus — same tradeoff as the
// shard snapshot's `live` policy.
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

// Dropdown-open "peek": soft-acknowledge every notification the user can see,
// shortening each unread one's per-user life to the reduced peek TTL and
// clearing the unread badge. The notifications service picks the affected set,
// so the request carries only the user id.
export const notificationMarkPeeked = defineWrite({
  subject: `${SUB.notifications}.mark_peeked`,
  request: (userId: string) => ({ user_id: userId }),
  after: (_result: unknown, userId: string) => invalidate(`notifications:${userId}`)
});

// Irreversibly delete the user's own account (and their owned delegations,
// cleared server-side). The caller drops the session cookie after this resolves.
// Custom error mapping (throw on !ok) doesn't fit defineWrite's result shape
// cleanly, so this stays hand-written.
export async function deleteSelf(userId: string): Promise<void> {
  const r = await rpc<{ ok?: boolean; error?: string }>(`${SUB.dashboard}.delete_self`, {
    user_id: userId
  });
  if (!r.ok) throw new Error(r.error ?? 'delete failed');
  invalidateUser(userId);
}

/**
 * Subscribe to the cache-invalidation bus so writes in other services (Go)
 * push-drop the affected keys without waiting on TTL expiry. Call once at
 * server boot (hooks.server.ts init). Scope -> key routing is the SCOPES map
 * above; the shared router owns transport, parsing, retry, and gap flushes.
 */
export function startInvalidationListener(): void {
  fabric.start();
}
