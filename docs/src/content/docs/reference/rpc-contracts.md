---
title: RPC contracts
description: Every NATS request-reply endpoint in ItsBagelBot. Subjects, JSON request/reply shapes, owners, and timeouts.
---

All inter-service calls use **NATS request-reply** with JSON bodies. Services
subscribe with a **queue group** so any replica can answer and load spreads
across the fleet. This page lists every live endpoint; subjects are env-overridable
but the defaults below are what ships.

## Conventions

- **Encoding**: JSON, UTF-8. Empty request bodies are allowed where the table says `{}`.
- **IDs**: Twitch user / broadcaster IDs are sent as **decimal strings**, not numbers.
- **Errors**: replies carry an `error` string field. It is empty/omitted on success;
  a non-empty value means the call failed (the transport still succeeded).
- **Tier vs status**: `status` is the raw DB enum (`free` | `paid` | `vip`).
  `tier` is derived: `premium` when paid or vip and active, else `standard`.
- **Timeouts**: callers set a request deadline (commonly 5s); handlers bound their
  own work with a context timeout (listed per group). A handler timeout shorter than
  the caller's is intentional so the caller still gets an error reply.

## Subject map

| Subject (default) | Owner | Kind |
|---|---|---|
| `bagel.rpc.dashboard.*` | users | request-reply |
| `bagel.rpc.commands.*` | commands | request-reply |
| `bagel.rpc.admin.user.*` | users | request-reply |
| `bagel.rpc.broadcaster.status.get` | projector | request-reply |
| `bagel.rpc.internal.projection.users.get` | users | request-reply |
| `bagel.rpc.internal.projection.modules.get` | modules | request-reply |
| `bagel.rpc.internal.projection.commands.get` | commands | request-reply |
| `bagel.rpc.internal.tokens.*` | users | request-reply |
| `bagel.rpc.outgress.{channel,system}.*` | outgress | request-reply |
| `twitch.ingress.admin.shards.get` | ingress | request-reply |
| `bagel.cache.invalidate.broadcaster` | users (pub) | event (fire-and-forget) |

---

## Dashboard (users) — `bagel.rpc.dashboard.*`

Broadcaster self-serve, called by the console dashboard. Queue group `users-rpc`.
Handler timeout **3s**. `broadcaster_user_id` is a decimal string.

| Verb | Request | Reply |
|---|---|---|
| `upsert_user` | `{user_id, username, display_name}` | `{ok:true}` or `{error}` |
| `grant_save` | `{broadcaster_user_id, access_token, refresh_token}` | `{ok:true}` or `{error}`; also publishes cache invalidation |
| `grant_has` | `{broadcaster_user_id}` | `{has_grant:bool}` |
| `active_set` | `{broadcaster_user_id, active:bool}` | `{ok:true}` or `{error}`; publishes cache invalidation |
| `active_get` | `{broadcaster_user_id}` | `{active:bool}` |
| `status_get` | `{broadcaster_user_id}` | `{status}` (raw enum) |

## Commands — `bagel.rpc.commands.*`

Custom chat commands for a broadcaster, called by the console dashboard. Queue
group `commands-rpc`. Handler timeout **2s**. `user_id` is a decimal string.

Shared reply shape:

```json
{ "commands": [ {"name": "...", "response": "...", "is_active": true} ], "error": "" }
```

| Verb | Request | Notes |
|---|---|---|
| `list` | `{user_id}` | returns the current command set |
| `upsert` | `{user_id, name, response, is_active}` | write-behind (~2s); reply is an **optimistic** merged list. A validation error returns the error alongside the unmodified list |
| `delete` | `{user_id, name}` | immediate; invalidates cache, so the returned list is fresh |

## Admin users — `bagel.rpc.admin.user.*`

Operator user management, called by the admin console / legacy admin tool. Queue
group `users-rpc`. Handler timeout **3s**. The admin tool never opens the DB; this
is its only door.

Shared reply shape (fields present per verb):

```json
{
  "user":  {"id": 1, "username": "x", "is_active": true, "status": "paid", "updated_at": "..."},
  "users": [ /* same shape */ ],
  "stats": {"total_users": 0, "active_users": 0, "premium_users": 0, "vip_users": 0, "paid_users": 0},
  "token": {"present": true},
  "error": ""
}
```

| Verb | Request | Returns |
|---|---|---|
| `get` | `{user_id}` or `{username}` | `user` |
| `list` | `{limit}` (1–100, default 20) | `users`, most-recently-updated first |
| `stats` | `{}` | `stats` |
| `set_status` | `{user_id, status}` (`free`/`paid`/`vip`) | `user`; provisions the row if unseen; invalidates cache |
| `reset` | `{user_id}` | `user`; clears the user's tokens |
| `token_set` | `{user_id, access_token, refresh_token}` | `token`; provisions if unseen (used to install the bot account token) |
| `token_status` | `{user_id}` | `token` (presence only, never the token value) |
| `token_clear` | `{user_id}` | `token`; deletes the stored token; invalidates cache |
| `delete` | `{user_id}` or `{username}` | empty reply; cascade-deletes the user |

## Broadcaster tier — `bagel.rpc.broadcaster.status.get`

Hot-path tier lookup served by the projector. Queue group `projector-rpc`. Handler
timeout **1.5s**. Resolution order: in-process cache (30s TTL) → Valkey projection
→ lazy-load fallback to the users projection RPC (result cached). Never errors the
caller for a missing user: unknown/invalid resolves to `standard`.

| Request | Reply |
|---|---|
| `{broadcaster_id}` | `{broadcaster_id, tier}` where `tier` is `premium` or `standard` |

## Internal projections — `bagel.rpc.internal.projection.*.get`

Read-through views used by the projector to materialize the settings projection on
stream-online. Handler timeout **2s**. `user_id` is a decimal string.

| Subject | Owner / queue group | Reply |
|---|---|---|
| `...projection.users.get` | users / `users-rpc` | `{user_id, status, is_active, error}` |
| `...projection.modules.get` | modules / `modules-rpc` | `{user_id, modules: [ModuleView], error}` |
| `...projection.commands.get` | commands / `commands-rpc` | `{user_id, commands: [CommandView], error}` |

View shapes:

```json
CommandView { "name": "...", "response": "...", "is_active": true }
ModuleView  { "name": "...", "is_enabled": true, "configs": { /* raw JSON, omitted if empty */ } }
```

## Internal tokens — `bagel.rpc.internal.tokens.*`

Bot-account Twitch token lifecycle. Outgress reads the bot refresh token at renewal
time and writes the rotated token back so a restart never resurrects a stale one.
**Plaintext tokens transit these subjects**; NATS authorization restricts who may
subscribe. Owner users, queue group `users-rpc`, handler timeout **3s**.

| Verb | Request | Reply |
|---|---|---|
| `get` | `{user_id}` | `{access_token, refresh_token, error}` |
| `save` | `{user_id, access_token, refresh_token}` | `{}` or `{error}` |

## Outgress management — `bagel.rpc.outgress.*`

Operator control of the sender. Queue group `outgress-rpc`. Handler timeout **1.5s**.

| Subject | Request | Reply |
|---|---|---|
| `...channel.get` | `{broadcaster_id}` | `{channel, found, error}` |
| `...channel.set` | `{broadcaster_id, enabled?, is_mod?}` | `{channel, found, error}`; creates the channel if absent; an `is_mod` override counts as a verification |
| `...channel.list` | `{}` | `{channels, error}` |
| `...system.status` | `{}` | `{paused, app_token_expires_in_seconds, has_user_token, error}` |
| `...system.pause` | `{paused:bool}` | `{paused, error}` (kill switch) |

`enabled` and `is_mod` are **optional** (pointer/omitted) on `channel.set`; absent
means leave unchanged. `channel`:

```json
{ "broadcaster_id": "...", "enabled": true, "is_mod": false,
  "mod_checked_at": "...", "updated_at": "..." }
```

## Ingress shard snapshot — `twitch.ingress.admin.shards.get`

Live view of the EventSub shard fleet, served by any ingress replica (Elixir).
Caller timeout **5s** (ingress caps per-shard work at ~2s). Request body is empty.

Reply (`Snapshot`):

```json
{
  "generated_at": "2026-06-15T00:00:00Z",
  "reporter": "ingress-node1",
  "nodes": ["node1", "node2"],
  "shard_count": 2,
  "conduit_manager": { "state": "...", "node": "...", "conduit_id": "..." },
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
`backoff`, `unregistered`, `unresponsive`. `conduit_manager` describes the
cluster-singleton conduit reconciler.

## Cache invalidation — `bagel.cache.invalidate.broadcaster`

Not request-reply: a fire-and-forget publish. Emitted by the users service after
any change that affects a broadcaster's cached state (status, active toggle, token
grant/clear). Body: `{broadcaster_id}` (decimal string). Subscribers (projector,
ingress) drop their cached view for that broadcaster and lazily reload.
