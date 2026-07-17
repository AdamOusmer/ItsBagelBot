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
import * as liveHub from './live-hub';
import { dashboardL1CacheCapacity } from './config-sanity';

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
  gateway: process.env.NATS_GATEWAY_SUBJECT_PREFIX ?? 'bagel.rpc.gateway',
  loyalty: process.env.NATS_LOYALTY_SUBJECT_PREFIX ?? 'bagel.rpc.loyalty',
  goveeKey: process.env.NATS_MODULES_GOVEE_SUBJECT_PREFIX ?? 'bagel.rpc.modules.govee',
  audit: process.env.NATS_ADMIN_AUDIT_SUBJECT_PREFIX ?? 'bagel.rpc.admin.user.audit',
  delegation: process.env.NATS_DELEGATION_SUBJECT_PREFIX ?? 'bagel.rpc.delegation',
  notifications: process.env.NATS_NOTIFICATIONS_SUBJECT_PREFIX ?? 'bagel.rpc.notifications',
  transactions: process.env.NATS_TRANSACTIONS_SUBJECT_PREFIX ?? 'bagel.rpc.transactions'
};

function userPrefixes(id: string): string[] {
  return [`grant:${id}`, `account:${id}`, `tier:${id}`, `billing-state:${id}`, `commands:${id}`, `modules:${id}`, `delegations:${id}`, `locale:${id}`, `cursor:${id}`, `govee-devices:${id}`];
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
  cursor: (id) => [`cursor:${id}`],
  '*': (id) => [...userPrefixes(id), `ban:${id}`]
};

// Hybrid read path: L1 SwrCache (+ push invalidation, SWR, stale-if-error) over
// per-key Valkey readers over RPC. Freshness policy per data class lives in the
// shared POLICY table; the bus, not the clock, is the main freshness lever.
// onInvalidation forwards each applied bus event to the live hub, which pushes it
// to that board's open browser SSE connections (see routes/events) so an open
// page re-fetches instantly — no client polling.
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

// Re-scope an existing grant (pending or consumed) to a new set of sections.
export async function delegationUpdate(ownerId: string, token: string, sections: string[]): Promise<void> {
  const r = await rpc<{ ok?: boolean; error?: string }>(`${SUB.delegation}.update`, {
    owner_user_id: ownerId,
    token,
    sections
  });
  if (!r.ok) throw new Error(r.error ?? 'update failed');
  invalidate(`delegation-token:${token}`, `delegations:${ownerId}`);
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

// Enqueue an ensure-optional job: outgress (re)creates only the optional
// subscriptions (the channel-points redemption sub) without touching the
// mandatory set. Idempotent (409) and non-affiliate-tolerant, so it is safe to
// fire after every reward create/enable — it is how a channel that just gained
// channel points (or just re-consented with the redemption scope) starts
// receiving redemption events without a full reconnect.
export async function publishEventSubEnsureOptional(broadcasterId: string): Promise<void> {
  await publish(SUB.outgress, {
    type: 'eventsub',
    broadcaster_id: broadcasterId,
    payload: { mode: 'ensure_optional' }
  });
}

export type ChannelSubState = {
  state: 'ok' | 'pending' | 'failing' | 'revoked' | 'unenrolled' | 'unknown';
  error: string;
  checkedAt: string | null;
};

function unknownSubState(): ChannelSubState {
  return { state: 'unknown', error: '', checkedAt: null };
}

// 'revoked' must stay a known state: folding it into 'unenrolled' would let
// the home page's self-heal publish enables against a channel outgress
// deliberately refuses to enroll until the broadcaster re-consents.
const KNOWN_SUB_STATES = ['ok', 'pending', 'failing', 'revoked'] as const;

function isKnownSubState(s: string): s is (typeof KNOWN_SUB_STATES)[number] {
  return (KNOWN_SUB_STATES as readonly string[]).includes(s);
}

// Read the persisted EventSub enroll state for a channel. Two distinct
// negatives: 'unenrolled' means outgress answered and holds no enrollment for
// this channel (never enrolled, or cleared by a disconnect) — callers may
// safely (re)enroll on it. 'unknown' is reserved for transport failure and
// fails safe: a transient outage never blocks page render and must never
// trigger writes. The fail-open catch doesn't fit defineRead cleanly (no cache
// involved either), so this stays hand-written.
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

export type AuditEntry = {
  actorId: string;
  actorLogin: string;
  action: string;
  target: string;
  detail: string;
};

// auditImpersonation records a dashboard write performed while an admin is
// viewing as the user. Best-effort: a logging failure must never block the
// action it describes, so callers fire-and-forget and we swallow errors here.
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
export type AccountState = { active: boolean; status: AccountStatus; onboarded: boolean; creatorCode: string | null };

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
  map: (r: { active: boolean; status: string; onboarded?: boolean; creator_code?: string | null }): AccountState => ({
    active: !!r.active,
    status: normalizeStatus(r.status),
    onboarded: !!r.onboarded,
    creatorCode: r.creator_code?.trim() ? r.creator_code : null
  }),
  timeoutMs: READ_TIMEOUT_MS,
  cache: {
    fabric,
    key: (userId: string) => `account:${userId}`,
    policy: POLICY.entity,
    l2: async (userId: string) => {
      const u = await valkey.getUser(userId);
      if (!u.known) return { hit: false, value: { active: false, status: 'free' as AccountStatus, onboarded: false, creatorCode: null } };
      // Valkey L2 does not cache onboarded or creator codes, so we fail the hit if we care about it,
      // but since it's just projected data, we'll let it pass or say hit: false if we must have onboarded.
      // Actually, since we need onboarded reliably on first load, and L2 is used for fast SSR, 
      // missing it in L2 means we should probably miss the cache to fetch it from L1/RPC.
      // For now, let's just return false and let SWR correct it if it was true, but this might flash.
      // Since it's only critical when false (to show modal), assuming true here would hide the modal until SWR finishes.
      // We will assume hit: false to force an RPC call to get the authoritative onboarded state.
      return { hit: false, value: { active: u.active, status: normalizeStatus(u.status), onboarded: false, creatorCode: null } };
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

// Single-user console preferences (locale, custom cursor). Each carries back on
// state_get and owns its own cache key with no Valkey L2 (the projected user
// hash has neither field); both are only read at login to seed a preference
// cookie, so a little staleness is harmless. prefRead/prefWrite capture the one
// shape both share so a new preference is a two-line declaration, not a copy.
function prefRead<T>(scope: string, map: (r: Record<string, unknown>) => T) {
  return defineRead<[string], Record<string, unknown>, T>({
    subject: `${SUB.dashboard}.state_get`,
    request: (userId: string) => ({ broadcaster_user_id: userId }),
    map,
    timeoutMs: READ_TIMEOUT_MS,
    cache: { fabric, key: (userId: string) => `${scope}:${userId}`, policy: POLICY.entity }
  });
}

// The console mirrors each choice into a cookie for an immediate flip; the
// write-through here is what makes it follow the account to another
// browser/device, and the `after` drop keeps this replica's cache honest.
function prefWrite<V>(verb: string, field: string, scope: string) {
  return defineWrite<[string, V], unknown>({
    subject: `${SUB.dashboard}.${verb}`,
    request: (userId: string, value: V) => ({ broadcaster_user_id: userId, [field]: value }),
    after: (_result: unknown, userId: string) => invalidate(`${scope}:${userId}`)
  });
}

export const userLocale = prefRead('locale', (r) => (typeof r.locale === 'string' && r.locale ? r.locale : 'en'));
export const setLocale = prefWrite<string>('locale_set', 'locale', 'locale');

// Cursor defaults to on when the field is absent (older accounts, failed read).
export const userCursor = prefRead('cursor', (r) => r.custom_cursor !== false);
export const setCursor = prefWrite<boolean>('cursor_set', 'custom_cursor', 'cursor');

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
