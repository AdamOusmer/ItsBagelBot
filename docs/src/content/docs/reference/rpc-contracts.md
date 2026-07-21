---
title: RPC contracts
description: Every NATS request-reply endpoint in ItsBagelBot, grouped by owning service, with subject, queue group, callers, and payload shape.
---

All inter-service calls use **NATS request-reply** with JSON bodies (`pkg/bus`). A business handler subscribes in a
per-service **queue group**, so any replica can answer and load spreads across the fleet. Subjects are
env-overridable, but the defaults below are what ships. This page is grounded in the per-service pages and
cross-checked against the account import and export lines in `deploy/k8s/nats-auth.conf`.

## Conventions

- **Encoding.** JSON, UTF-8. Empty request bodies are allowed for no-argument RPCs; the handler validates required
  fields on the zero-value request.
- **IDs.** Twitch user and broadcaster ids travel as decimal strings, not numbers.
- **Errors.** Replies carry an `error` string field, empty or omitted on success. `pkg/bus` normalizes a non-empty
  `{"error": "..."}` into a Go `RPCReplyError`, so a failed reply is never mistaken for a zero-valued success (an
  undecodable request body answers `{"error":"bad request"}`).
- **Tier versus status.** `status` is the raw database enum (`free` | `paid` | `vip`); `tier` is derived (`premium`
  when paid or vip and active, else `standard`).
- **Timeouts.** The caller sets a request deadline (the `pkg/bus` default is 5s); the handler bounds its own work
  with a context timeout (commonly 2s, 3s for the dashboard and admin-user verbs, 1.5s for status/live and outgress
  management, 15s for gateway and checkout). A handler over 250 ms logs at debug.
- **Account isolation.** Every service answers on its own `<SERVICE>_RPC` account and can call only the subjects its
  account imports. The health prefix `bagel.rpc.health.<service>` is a separate, side-effect-free, queue-balanced
  surface.

## Owner and account map

| Subject prefix | Owner (account) | Kind |
|---|---|---|
| `bagel.rpc.dashboard.*`, `bagel.rpc.admin.user.*`, `bagel.rpc.delegation.*` | users (`USERS_RPC`) | request-reply |
| `bagel.rpc.internal.{tokens,billing.apply,users.email.get,projection.users.get}` | users (`USERS_RPC`) | request-reply (export-scoped) |
| `bagel.rpc.broadcaster.status.get`, `bagel.rpc.broadcaster.live.get`, `bagel.rpc.projector.dashboard.*` | projector (`PROJECTOR_RPC`) | request-reply |
| `bagel.rpc.commands.*`, `bagel.rpc.internal.projection.commands.get` | commands (`COMMANDS_RPC`) | request-reply |
| `bagel.rpc.modules.*`, `bagel.rpc.internal.{projection.modules.get,govee.key.get}` | modules (`MODULES_RPC`) | request-reply |
| `bagel.rpc.loyalty.*` | loyalty (`LOYALTY_RPC`) | request-reply |
| `bagel.rpc.outgress.*` | outgress (`OUTGRESS_RPC`) | request-reply |
| `bagel.rpc.transactions.*` | transactions (`TRANSACTIONS_RPC`) | request-reply |
| `bagel.rpc.notifications.*`, `bagel.rpc.admin.notifications.*` | notifications (`NOTIFICATIONS_RPC`) | request-reply |
| `bagel.rpc.gateway.*` | gateway (`GATEWAY_RPC`) | request-reply |
| `twitch.ingress.admin.shards.*`, `bagel.rpc.ingress.conduit.get` | twitch-ingress (`TWITCH_INGRESS_RPC`) | request-reply |
| `bagel.rpc.health.*` | every service | request-reply (health) |
| `bagel.cache.invalidate.<scope>` | scope owner | event (fire-and-forget) |

## Users, queue group `users-rpc`

### Dashboard, `bagel.rpc.dashboard.*` (handler 3s)

Called by the console dashboard; `bagel.rpc.dashboard.state_get` is also imported by outgress (reauth localization).

| Verb | Request | Reply |
|---|---|---|
| `upsert_user` | `{user_id, username, email?}` | `{ok}` (register on first sight, best-effort email capture) |
| `grant_save` | `{broadcaster_user_id, access_token, refresh_token}` | `{ok}` (seals and upserts the token) |
| `grant_has` | `{broadcaster_user_id}` | `{has_grant}` |
| `active_set` / `active_get` | `{broadcaster_user_id[, active]}` | `{ok}` / `{active}` |
| `status_get` / `state_get` | `{broadcaster_user_id}` | view fields (`state_get` coalesces active, tier, preferences, billing) |
| `onboarded_set` / `locale_set` / `cursor_set` | `{broadcaster_user_id, ...}` | `{ok}` |
| `delete_self` | `{user_id}` | `{ok}` (deletes the user and their delegations) |

### Admin user management, `bagel.rpc.admin.user.*` (handler 3s)

Called by the console admin; `bagel.rpc.admin.user.get` is also imported by transactions (gift vetting) and
notifications (username targeting); `bagel.rpc.admin.user.audit.append` by the dashboard (view-as writes).

| Verb | Purpose |
|---|---|
| `get` / `list` / `stats` / `overview` / `enrollment` | Fetch, paged search, tier counts, signup histogram |
| `set_status` / `set_active` / `set_creator_code` | Operator writes on tier, receive toggle, public creator code |
| `ban` / `unban` | Block or unblock (ingress drops banned users) |
| `reset` / `token_set` / `token_status` / `token_clear` | Bot-account token custody |
| `delete` | Remove a user row |
| `auth.check` / `auth.list` / `auth.upsert` / `auth.remove` | DB-backed staff roster under the role ladder |
| `audit.append` / `audit.list` | Append and page the operator audit log |

### Delegation, `bagel.rpc.delegation.*` (handler 3s)

Called by the console dashboard: `create`, `get`, `consume`, `list`, `revoke`, `update`, `access`, `opt_out`.
Mutations publish a `delegation`-scope cache invalidation.

## Projector, queue group `projector-rpc`

| Subject | Request | Reply | Caller(s) |
|---|---|---|---|
| `bagel.rpc.broadcaster.status.get` | `{broadcaster_id}` | `{broadcaster_id, tier, banned}` | sesame, twitch-ingress, dashboard (handler 1.5s) |
| `bagel.rpc.broadcaster.live.get` | `{broadcaster_id}` | `{broadcaster_id, live, known}` | sesame (handler 1.5s) |
| `bagel.rpc.projector.dashboard.commands.get` / `modules.get` | `{user_id}` | `{user_id, commands}` / `{user_id, modules}` | dashboard, sesame (cold-cache fallback; handler 2s) |
| `bagel.rpc.projector.dashboard.commands.replace` / `modules.replace` | `{user_id, commands}` / `{user_id, modules}` | echoed list (write on a bounded gate) | dashboard (handler 2s) |

## Commands, queue group `commands-rpc` (handler 2s)

| Subject | Request / reply | Caller(s) |
|---|---|---|
| `bagel.rpc.commands.list` | `DashboardRequest` / `DashboardReply` (command set) | dashboard |
| `bagel.rpc.commands.upsert` | `DashboardRequest` / `DashboardReply` (a differing `original_name` renames in place) | dashboard, sesame (`!cmd`) |
| `bagel.rpc.commands.delete` | `DashboardRequest` / `DashboardReply` | dashboard, sesame (`!cmd`) |
| `bagel.rpc.internal.projection.commands.get` | `projection.Request` / `CommandsReply` | projector (export-scoped) |

## Modules, queue group `modules-rpc` (handler 2s)

| Subject | Request / reply | Caller(s) |
|---|---|---|
| `bagel.rpc.modules.list` / `upsert` / `patch` | `DashboardRequest` / `DashboardReply` (`patch` runs a revision compare-and-swap) | dashboard |
| `bagel.rpc.modules.quote.add` / `get` / `random` / `remove` / `list` | `QuoteRequest` / `QuoteReply` | sesame (`!quote`) |
| `bagel.rpc.modules.personality.feed` | `FeedBumpRequest` / `FeedBumpReply` | sesame (feed counter) |
| `bagel.rpc.modules.govee.set` / `clear` / `status` | govee requests / replies (never echoes the key) | dashboard |
| `bagel.rpc.internal.projection.modules.get` | `projection.Request` / `ModulesReply` | projector (export-scoped) |
| `bagel.rpc.internal.govee.key.get` | `KeyGetRequest` / `KeyGetReply` (decrypted key) | gateway only (export-scoped) |

## Loyalty, queue group `loyalty-rpc` (handler 2s)

All under `bagel.rpc.loyalty.*` (`loyaltyrpc.Request` / `loyaltyrpc.Reply`). Called by sesame on a cache miss and by
the console dashboard's loyalty tab.

| Verb | Request | Reply |
|---|---|---|
| `balance.get` | `user_id, viewer_id` | `balance` (zero when unseen) |
| `balance.set` / `balance.add` | `user_id, viewer_login, value` | `balance, found` (mod grant by login) |
| `top.get` | `user_id, limit` | `top` (leaderboard) |
| `counter.get` | `user_id, name [, viewer_id, command]` | `counter, found` |
| `counter.create` | `user_id, name, scope` | `counter` (idempotent) |
| `counter.set` | `user_id, name, value [, viewer_id, command]` | `found` (viewer 0 resets an entry scope) |
| `counter.delete` | `user_id, name` | `found` |
| `counter.list` / `counter.entries` | `user_id [, name, limit]` | `counters` / `entries, found` |

## Outgress, queue group `outgress-rpc` (handler 1.5s)

| Subject | Request | Reply | Caller(s) |
|---|---|---|---|
| `bagel.rpc.outgress.channel.get` | `{broadcaster_id}` | `{channel, found}` | dashboard, admin |
| `bagel.rpc.outgress.channel.set` / `channel.list` | `{broadcaster_id, enabled?, is_mod?}` / `{}` | `{channel, found}` / `{channels}` | operator (outgress account) |
| `bagel.rpc.outgress.system.status` / `system.pause` | `{}` / `{paused}` | status / `{paused}` (kill switch) | operator (outgress account) |
| `bagel.rpc.outgress.followage.get` | `{broadcaster_id, target_id?, target_login?}` | `{following, followed_at, user_found}` | sesame |
| `bagel.rpc.outgress.accountage.get` | `{target_id?, target_login?}` | `{created_at, user_found}` | sesame |
| `bagel.rpc.outgress.chatters.get` | `{broadcaster_id}` | `{chatters, missing_scope?}` | sesame (loyalty watch tick) |
| `bagel.rpc.outgress.channelpoints.list` / `create` / `update` / `delete` | `{broadcaster_id, ...}` | `{rewards}` / `{reward, missing_scope?}` / `{}` | dashboard |

Outgress imports `bagel.rpc.internal.tokens.>` (users), `bagel.rpc.ingress.conduit.get` (twitch-ingress),
`bagel.rpc.dashboard.state_get` (users), and `bagel.rpc.admin.notifications.send` (notifications). Note the ACL
cross-check: the `system.*` and `channel.set` / `channel.list` verbs are exported under `bagel.rpc.outgress.>` but no
other RPC account currently imports them, so they are reachable only by a client on the outgress account itself.

## Transactions, queue group `transactions-rpc`

| Subject | Request | Reply | Caller |
|---|---|---|---|
| `bagel.rpc.transactions.basket_create` | `{user_id, username?, recipient_username?, ip_address?, package_type?, gift_message?}` | `{ident, checkout_url, recipient_login?, error?}` (handler 15s) | dashboard |

Transactions imports `bagel.rpc.admin.user.get`, `bagel.rpc.internal.billing.apply`,
`bagel.rpc.internal.users.email.get` (users), and `bagel.rpc.admin.notifications.send` (notifications). Entitlement
itself arrives over the Tebex HTTPS webhook (not NATS) and is applied through `billing.apply`.

## Notifications, queue group `notifications-rpc`

| Subject | Request | Reply | Caller(s) |
|---|---|---|---|
| `bagel.rpc.notifications.list` | `{user_id}` | `{notifications, unread_count}` | dashboard |
| `bagel.rpc.notifications.mark_read` | `{user_id, notification_id}` | `{}` | dashboard |
| `bagel.rpc.notifications.mark_peeked` | `{user_id}` | `{peeked}` | dashboard |
| `bagel.rpc.admin.notifications.send` | `SendRequest` (scope, target, title, body, level, `request_id?`) | `{notification}` | admin, transactions, outgress |
| `bagel.rpc.admin.notifications.list` / `delete` | `{page, limit}` / `{id}` | history / `{}` | admin |
| `bagel.rpc.internal.notifications.cleanup` | `{}` | `{deleted}` | the service's own creds (CronJob); not exported from the account |

Notifications imports `bagel.rpc.admin.user.get` (users) for username-to-id resolution.

## Gateway, queue group `gateway-rpc`

Every endpoint is `bagel.rpc.gateway.<provider>.<endpoint>` and takes the shared `gatewayrpc.Request` (fields per
endpoint, `is_premium` selects the rate lane). Called by sesame for chat commands; the console dashboard calls only
`govee.devices`. Providers register only when configured, so an unconfigured provider's subjects time out at the
caller. Default handler timeout is 15s (govee `devices` 8s, `control` 12s).

| Subject | Request fields | Reply type |
|---|---|---|
| `bagel.rpc.gateway.urchin.daily` / `weekly` / `monthly` | `account`, `is_premium` | `UrchinSessionReply` |
| `bagel.rpc.gateway.urchin.sniper` / `tags` | `account`, `is_premium` | `UrchinSniperReply` / `UrchinTagsReply` |
| `bagel.rpc.gateway.hypixel.stats` | `account`, `is_premium` | `HypixelStatsReply` |
| `bagel.rpc.gateway.mcsr.user` / `session_start` / `session` | `account`, `channel_id?`, `is_premium` | `McsrUserReply` / `McsrSnapshotReply` / `McsrSessionReply` |
| `bagel.rpc.gateway.fortnite.shop` / `stats` / `session_start` / `session` | `account?`, `channel_id?`, `is_premium` | `FortniteShopReply` / `FortniteStatsReply` / snapshot / session |
| `bagel.rpc.gateway.govee.devices` / `control` | `channel_id`, device fields | `GoveeDevicesReply` / `GoveeControlReply` |

Gateway imports one subject only: `bagel.rpc.internal.govee.key.get` (modules), the decrypted per-broadcaster Govee
key.

## Twitch Ingress, queue group `twitch-ingress-admin`

Any replica answers via the Horde registry, so exactly one replies per request.

| Subject | Request | Reply | Caller |
|---|---|---|---|
| `twitch.ingress.admin.shards.get` | ignored | full cluster and shard snapshot | admin |
| `twitch.ingress.admin.shards.scale` | `{count}` | snapshot, or `{error}` | admin |
| `twitch.ingress.admin.shards.autoscale` | `{enabled}` | snapshot, or `{error}` | admin |
| `bagel.rpc.ingress.conduit.get` | `{}` | `{conduit_id}` | outgress |

Ingress imports `bagel.rpc.broadcaster.status.get` (projector) for lane resolution and subscribes (broadcast, no
queue group) to `bagel.cache.invalidate.status` to evict its lane cache. The shard `state` in a snapshot is one of
`connected`, `migrating`, `binding`, `connecting`, `backoff`, `unregistered`, `unresponsive`.

## Sesame

Sesame serves no business RPC; it is a client of the surfaces above. It exposes only `bagel.rpc.health.sesame` on
queue group `sesame-rpc`.

## Export-scoped internal subjects

These carry secrets or projection reads and are import-gated at the account level to a single caller (or a short
list), so no other account can subscribe to them even by accident.

| Subject | Exported by | Imported by | Carries |
|---|---|---|---|
| `bagel.rpc.internal.tokens.get` / `save` | `USERS_RPC` | `OUTGRESS_RPC` only | plaintext Twitch tokens (bot and per-broadcaster) |
| `bagel.rpc.internal.billing.apply` | `USERS_RPC` | `TRANSACTIONS_RPC` only | verified Tebex entitlement application |
| `bagel.rpc.internal.users.email.get` | `USERS_RPC` | `TRANSACTIONS_RPC` only | decrypted contact email for gift mail |
| `bagel.rpc.internal.projection.users.get` | `USERS_RPC` | `PROJECTOR_RPC`, `WORKER_RPC` | user projection (tier, active, banned, locale) |
| `bagel.rpc.internal.projection.commands.get` | `COMMANDS_RPC` | `PROJECTOR_RPC` | command projection hydration |
| `bagel.rpc.internal.projection.modules.get` | `MODULES_RPC` | `PROJECTOR_RPC` | module projection hydration |
| `bagel.rpc.internal.govee.key.get` | `MODULES_RPC` | `GATEWAY_RPC` only | decrypted per-broadcaster Govee key |
| `bagel.rpc.internal.notifications.cleanup` | not exported | notifications' own creds (CronJob) | TTL sweep trigger |

## Health probes

Every service exports `bagel.rpc.health.<service>` (`bagel.rpc.health.{users,commands,modules,loyalty,projector,`
`outgress,transactions,notifications,gateway,sesame,ingress}`), queue-balanced on its own RPC group. The reply is a
tiny `{service, ok:true}`; the useful measurement is the NATS round trip at the caller, and the responder reads no
database and calls no upstream, so a latency sample never has side effects. The console admin Analytics page imports
all of them.

## Cache invalidation bus (event, not request-reply)

Not request-reply: a fire-and-forget publish, subscribed with no queue group so every replica hears every message
and evicts its own in-process cache. The subjects are scope-per-subject under the prefix `bagel.cache.invalidate`
(there is no single `bagel.cache.invalidate.broadcaster` subject; the tier and ban scope is `status`). Each scope is
exported by its owner and imported only by the accounts that cache it.

| Subject | Exported by | Consumed by |
|---|---|---|
| `bagel.cache.invalidate.status` | users | sesame, dashboard, admin, twitch-ingress |
| `bagel.cache.invalidate.grant` | users | sesame, dashboard, admin |
| `bagel.cache.invalidate.locale` | users | sesame, dashboard |
| `bagel.cache.invalidate.delegation` | users | dashboard |
| `bagel.cache.invalidate.commands` | projector | sesame, dashboard |
| `bagel.cache.invalidate.modules` | projector | sesame, dashboard |
| `bagel.cache.invalidate.live` | projector | sesame |
| `bagel.cache.invalidate.notifications` | notifications | dashboard, admin |

Outgress additionally publishes `bagel.cache.invalidate.outgress` and `.outgress-pause` for its own replicas, users
publishes `.cursor` and a coarse `.user` scope, and the projector fans its in-process tier eviction on the separate
core subject `bagel.internal.projector.tier.invalidate`. The payload throughout is `{broadcaster_id}` (or `*` for a
broadcast flush).
