---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: RPC contracts
description: Every NATS request-reply endpoint in ItsBagelBot. Subjects, JSON request/reply shapes, owners, and timeouts.
---

All inter-service calls use **NATS request-reply** with JSON bodies. Services
subscribe with a **queue group** so any replica can answer and load spreads
across the fleet. This page lists the core endpoints; subjects are env-overridable
but the defaults below are what ships. The per-account import lists in
`deploy/messaging/accounts.yaml` show which service may call which subject.

## Conventions

- **Encoding**: JSON, UTF-8. Empty request bodies are allowed where the table says `{}`.
- **IDs**: Twitch user / broadcaster IDs are sent as **decimal strings**, not numbers.
- **Errors**: replies carry an `error` string field. It is empty/omitted on success;
  a non-empty value means the call failed (the transport still succeeded). Most
  data-service replies also carry a `code` (`invalid`, `not_found`, `forbidden`,
  `conflict`, `unavailable`, `internal`) from `internal/domain/rpc/code.go`.
  Projector and outgress management replies carry `error` only.
- **Tier vs status**: `status` is the raw DB enum (`free` | `paid` | `vip`).
  `tier` is derived: `premium` when status is paid or vip, else `standard`.
- **Timeouts**: callers set a request deadline (commonly 5s); handlers bound their
  own work with a context timeout (default 2s, `pkg/bus/rpcwiring.go`; listed per group). A handler timeout shorter than
  the caller's is intentional so the caller still gets an error reply.

## Subject map

| Subject (default) | Owner | Kind |
|---|---|---|
| `bagel.rpc.dashboard.*` | users | request-reply |
| `bagel.rpc.commands.*` | commands | request-reply |
| `bagel.rpc.admin.user.*` | users | request-reply |
| `bagel.rpc.broadcaster.{status,live,stream_info}.get` | projector | request-reply |
| `bagel.rpc.projector.dashboard.*` | projector | request-reply |
| `bagel.rpc.internal.projection.users.get` | users | request-reply |
| `bagel.rpc.internal.projection.modules.get` | modules | request-reply |
| `bagel.rpc.internal.projection.commands.get` | commands | request-reply |
| `bagel.rpc.internal.tokens.*` | users | request-reply |
| `bagel.rpc.outgress.*` | outgress | request-reply |
| `twitch.ingress.admin.shards.get` | ingress | request-reply |
| `bagel.cache.invalidate.<scope>` | users, projector, others (pub) | event (fire-and-forget) |

Other families, each with its own context file under `app/`:

| Subject prefix | Owner |
|---|---|
| `bagel.rpc.modules.*` (including `.govee`, `.spotify`) | modules |
| `bagel.rpc.loyalty.*` | loyalty |
| `bagel.rpc.transactions.*`, `bagel.rpc.admin.giveaways.*`, `bagel.rpc.internal.billing.apply` | transactions (billing apply is served by users) |
| `bagel.rpc.notifications.*`, `bagel.rpc.admin.notifications.*` | notifications |
| `bagel.rpc.delegation.*` | users |
| `bagel.rpc.discord-data.*` | discord-data |
| `bagel.rpc.discord-outgress.*`, `bagel.rpc.dingress.*` (older name kept for wire compatibility) | discord outgress |
| `bagel.rpc.gossip.*` | gossip |
| `bagel.rpc.admin.deploy.*` | deployer |
| `bagel.rpc.health.<service>` | every service |

---

## Dashboard (users): `bagel.rpc.dashboard.*`

Broadcaster self-serve, called by the console dashboard. Queue group `users-rpc`.
Handler timeout **3s**. `broadcaster_user_id` is a decimal string.

| Verb | Request | Reply |
|---|---|---|
| `upsert_user` | `{user_id, username, display_name, email?}` | `{ok:true}` or `{error}`; publishes status invalidation |
| `grant_save` | `{broadcaster_user_id, access_token, refresh_token}` | `{ok:true}` or `{error}`; also publishes cache invalidation |
| `grant_has` | `{broadcaster_user_id}` | `{has_grant:bool}` |
| `active_set` | `{broadcaster_user_id, active:bool}` | `{ok:true}` or `{error}`; publishes cache invalidation |
| `active_get` | `{broadcaster_user_id}` | `{active:bool}` |
| `status_get` | `{broadcaster_user_id}` | `{status, onboarded}` (status is the raw enum) |
| `state_get` | `{broadcaster_user_id}` | account state: `active`, `username`, `display_name`, `status`, `onboarded`, `locale`, `custom_cursor`, `commands_page_hidden`, `creator_code`, plus subscription fields |
| `login_resolve` | `{login}` | `{user_id, username}` |
| `onboarded_set` | `{broadcaster_user_id, onboarded}` | `{ok:true}` or `{error}` |
| `locale_set` | `{broadcaster_user_id, locale}` | `{ok:true}` or `{error}` |
| `cursor_set` | `{broadcaster_user_id, custom_cursor}` | `{ok:true}` or `{error}` |
| `commands_page_set` | `{broadcaster_user_id, commands_page_hidden}` | `{ok:true}` or `{error}`; hides or shows the public commands page |
| `delete_self` | `{user_id}` | `{ok:true}` or `{error}` |

## Commands: `bagel.rpc.commands.*`

Custom chat commands for a broadcaster, called by the console dashboard. Queue
group `commands-rpc`. Handler timeout **2s** (the default). `user_id` is a decimal string.

Request fields: `user_id`, `name`, `aliases`, `response`, `is_active`,
`stream_online_only`, `perm`, `cooldown`, `user_cooldown`, `allowed_user_id`,
`bump_counter`, `original_name?`, `restore_uses?`. Reply shape:

```json
{ "commands": [ /* CommandView */ ], "restored": false, "error": "", "code": "" }
```

| Verb | Request | Notes |
|---|---|---|
| `list` | `{user_id}` | returns the current command set |
| `upsert` | the fields above | write-behind (~2s). The reply carries no list; read the set from the projector dashboard subjects. When `original_name` is set and differs from `name`, the row is **renamed in place** (row identity kept), immediately and not write-behind. When `restore_uses` is positive and `original_name` is empty, a previously deleted command is restored with its use count and `restored` is set |
| `delete` | `{user_id, name}` | immediate; invalidates the cache and publishes a deletion event |

Source: `app/db/commands/rpc/dashboard.go`, `internal/domain/rpc/commands/commands.go`.

## Admin users: `bagel.rpc.admin.user.*`

Staff user management, called by the admin console. Queue group `users-rpc`.
Handler timeout **3s**. Every verb takes `actor_id`; the service resolves the
actor's staff role itself and refuses with `forbidden` below the verb's minimum
role. The `actor_role` field is client-supplied and never trusted. The console
never opens the database; this is its only door.

Reply fields (present per verb): `user`, `users`, `stats`, `enrollment`, `token`,
paging fields (`page`, `page_size`, `max_pages`, `has_more`), plus `error` and `code`.

| Verb | Minimum role | Returns |
|---|---|---|
| `get`, `list`, `stats`, `enrollment`, `overview`, `token_status` | moderator | `user`, `users`, `stats`, `enrollment` or `token` (presence only, never the token value) |
| `ban`, `unban` | moderator | `user` |
| `set_status`, `set_active`, `set_creator_code`, `test.set`, `reset` | admin | `user` |
| `token_set`, `token_clear` | admin | `token` |
| `bot_token_set` | grant check | `token`; installs the bot account token after Twitch validates its identity and scopes |
| `delete` | owner | empty reply; cascade-deletes the user |

The staff roster and audit log live under `bagel.rpc.admin.user.auth.*` and
`bagel.rpc.admin.user.audit.*`. Source: `app/db/users/rpc/admin.go`,
`internal/domain/rpc/users/users.go`.

## Broadcaster status: `bagel.rpc.broadcaster.{status,live,stream_info}.get`

Hot-path lookups served by the projector. Queue group `projector-rpc`.
Handler timeout **1.5s** (`app/projector/rpc/`).

`status.get` resolution order: in-process cache (30s TTL) then Valkey projection
then a lazy-load fallback to the users projection RPC (result cached). Unknown or
invalid broadcasters resolve to `standard`; a lookup failure also resolves to
`standard` with an `error` string.

| Subject | Request | Reply |
|---|---|---|
| `...status.get` | `{broadcaster_id}` | `{broadcaster_id, tier, banned, error?}` where `tier` is `premium` or `standard` |
| `...live.get` | `{broadcaster_id}` | `{broadcaster_id, live, known, version?, error?}` |
| `...stream_info.get` | `{broadcaster_id}` | `{broadcaster_id, title, game_name, viewer_count, peak_viewers, started_at, ended_at, live, known, error?}` |

The projector also serves the dashboard read model under
`bagel.rpc.projector.dashboard.{commands,modules}.{get,replace}` (handler timeout 2s).

## Internal projections: `bagel.rpc.internal.projection.*`

Read-through views the projector uses to hydrate the settings projection.
Handler timeout **2s**. `user_id` is a decimal string.

| Subject | Owner / queue group | Reply |
|---|---|---|
| `...projection.users.get` | users / `users-rpc` | `{user_id, status, is_active, banned, locale?, commands_page_hidden?, state_revision?, account_created_at?, error}` |
| `...projection.modules.get` | modules / `modules-rpc` | `{user_id, modules: [ModuleView], error}` |
| `...projection.commands.get` | commands / `commands-rpc` | `{user_id, commands: [CommandView], error}` |
| `...projection.commands.fetches.get` | commands / `commands-rpc` | custom-fetch definitions for the user |

View shapes (see `internal/domain/rpc/projection/projection.go` for the full field set):

```json
CommandView { "name": "...", "aliases": [], "response": "...", "is_active": true, "perm": "everyone", "cooldown": 0 }
ModuleView  { "name": "...", "is_enabled": true, "configs": { /* raw JSON, omitted if empty */ }, "revision": 0 }
```

## Internal tokens: `bagel.rpc.internal.tokens.*`

Twitch user-token lifecycle. Outgress reads the stored refresh token at renewal
time and writes the rotated token back so a restart never resurrects a stale one.
**Plaintext tokens transit these subjects**; NATS authorization restricts who may
subscribe. Owner users, queue group `users-rpc`, handler timeout **2s**.

| Verb | Request | Reply |
|---|---|---|
| `get` | `{user_id}` | `{access_token, refresh_token, access_token_expires_at?, error, code}` |
| `save` | `{user_id, access_token, refresh_token, access_token_expires_at?}` | `{}` or `{error, code}` |

## Outgress management: `bagel.rpc.outgress.*`

Operator control of the sender and Twitch lookups. Queue group `outgress-rpc`.
Handler timeout **1.5s**; lookups that call Twitch (`followage`, `uptime`,
`streaminfo`, `channelcounts`) use **4s**, and `channel.get` uses **6s** so a live verify fits.
Each subject runs on a bounded concurrent worker pool, so one slow call never blocks the others.
Source: `app/twitch/outgress/rpc/manage.go`.

| Subject | Request | Reply |
|---|---|---|
| `...channel.get` | `{broadcaster_id, verify?, repair?}` | `{channel, found, error, verification?}`; `verify` lists the channel's live Twitch subscriptions (at most once per 30s per channel), records `ok` or `failing`, and `repair` re-enrolls missing ones in the background; caller timeout **8s** when verifying |
| `...channel.set` | `{broadcaster_id, enabled?, is_mod?}` | `{channel, found, error}`; creates the channel if absent; an `is_mod` override counts as a verification |
| `...channel.list` | `{}` | `{channels, error}` |
| `...system.status` | `{}` | `{paused, app_token_expires_in_seconds, has_user_token, error}` |
| `...system.pause` | `{paused:bool}` | `{paused, error}` (kill switch) |
| `...followage.get`, `...accountage.get`, `...uptime.get`, `...streaminfo.get`, `...channelcounts.get` | lookup-specific | Twitch-derived values for chat commands and variables |

Channel-point rewards and chatter lists are served from the same prefix
(`app/twitch/outgress/rpc/channelpoints.go`, `chatters.go`). The trial
subscription call uses `bagel.rpc.outgress.trial_subscription` on its own queue group.

`enabled` and `is_mod` are **optional** (pointer/omitted) on `channel.set`; absent
means leave unchanged. `channel`:

```json
{ "broadcaster_id": "...", "enabled": true, "is_mod": false,
  "mod_checked_at": "...", "updated_at": "...",
  "sub_state": "...", "sub_error": "", "sub_checked_at": "...",
  "blocked_at": "...", "grant_state": "", "grant_checked_at": "..." }
```

`verification`:

```json
{ "expected": 9, "present": 8, "missing": ["channel.cheer"],
  "repairing": false, "throttled": false, "error": "" }
```

## Ingress shard snapshot: `twitch.ingress.admin.shards.get`

Live view of the EventSub shard fleet, served by any ingress replica (Elixir).
Caller timeout **5s** (ingress caps per-shard work at ~2s). Request body is empty.

The same service also answers `twitch.ingress.admin.shards.scale`,
`twitch.ingress.admin.shards.autoscale`, `twitch.ingress.admin.trials.{list,add,remove,set_enabled}`
and `bagel.rpc.ingress.conduit.get` (`app/twitch/ingress/config/runtime.exs`,
`app/twitch/ingress/lib/ingress/application.ex`).

Reply (`Snapshot`), abridged. Further top-level fields are `capacity`,
`desired_count`, `target`, `min_shards`, `max_shards`, `autoscale`, `max_load`,
`max_load_shard_id`, `trial_loads`, `trial_burst_loads` and `trial_sockets`
(`app/twitch/ingress/lib/ingress/admin_rpc.ex`):

```json
{
  "generated_at": "2026-06-15T00:00:00Z",
  "reporter": "ingress-node1",
  "nodes": ["node1", "node2"],
  "shard_count": 2,
  "conduit_manager": { "state": "...", "node": "...", "conduit_id": "..." },
  "inventory": "available",
  "shards": [
    {
      "shard_id": 0, "state": "connected", "node": "node1",
      "session_id": "...", "bound": true, "handshake_in_flight": false,
      "keepalive_ms": 10000, "attempts": 0,
      "bound_at": "...", "last_frame_at": "..."
    }
  ]
}
```

Shard `state` is one of: `connected`, `migrating`, `binding`, `connecting`,
`backoff`, `unregistered`, `unresponsive`, `unknown`. `conduit_manager` describes the
cluster-singleton conduit reconciler. `inventory` is `unavailable` when the supervisor
children listing timed out; every shard not otherwise known is then `unknown`.

## Cache invalidation: `bagel.cache.invalidate.<scope>`

Not request-reply: a fire-and-forget publish. The subject suffix is the scope,
for example `status`, `grant`, `modules`, `delegation`, `notifications` or the
live-state scope. Owners publish after any change that affects a broadcaster's
cached state (status, active toggle, token grant or clear, projection writes).
Body: `{broadcaster_id, keys?}` (`broadcaster_id` is a decimal string,
`internal/domain/invalidate/invalidate.go`). Subscribers (projector, ingress,
sesame and others) drop their cached view and lazily reload.
