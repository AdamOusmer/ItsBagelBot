---
title: Service registry
description: The services that make up ItsBagelBot, what they own, how they talk, and how they authenticate to each other.
---

Every service is independently deployable for zero-downtime rollouts. Go for the data and worker services,
Elixir/OTP for the Twitch ingress, SvelteKit (SSR) for the console, Astro for the marketing site. The only
inter-service transport is **NATS**: subject-based pub/sub for events, request-reply for RPC. No service reads
another service's database ([ADR 0007](/adr/0007-adoption-of-per-schema-data-microservices/)).

## Registry

| Service | Repo path | Language | Owns / does | Exposes on NATS |
|---|---|---|---|---|
| [Twitch Ingress](/microservices/twitch-ingress/) | `app/ingress/` | Elixir (OTP 27+) | EventSub Conduit and WebSocket shards; per-shard supervision and autoscaling; folds and lanes events by broadcaster status | publishes `twitch.ingress.event.*` (lanes) and `twitch.ingress.status.*`; serves `twitch.ingress.admin.shards.*`, `bagel.rpc.ingress.conduit.get` |
| [Sesame](/microservices/sesame/) | `app/sesame/` | Go | The production event worker: drains the lanes, moderates chat inline, dispatches commands, runs modules, publishes actions | consumes `twitch.ingress.event.{premium,standard}`; publishes `twitch.outgress.*`; emits `data.commands.used`, `data.loyalty.*`; serves `bagel.rpc.health.sesame` |
| [Outgress](/microservices/outgress/) | `app/outgress/` | Go | The sole egress to Twitch Helix; fleet-wide rate limiting; OAuth token lifecycle; EventSub enrollment; kill switch | consumes `twitch.outgress.*` and `twitch.ingress.event.stream`; serves `bagel.rpc.outgress.*` |
| [Projector](/microservices/projector/) | `app/projector/` | Go | Folds `data.*` change events into the Valkey settings projection; answers tier, live-state, and dashboard reads | serves `bagel.rpc.broadcaster.status.get`, `bagel.rpc.broadcaster.live.get`, `bagel.rpc.projector.dashboard.*` |
| [Users](/microservices/users/) | `app/users/` | Go | Owns `bagel_users`: accounts, tier status, the sealed OAuth token vault, staff roles and audit, delegations | serves `bagel.rpc.dashboard.*`, `bagel.rpc.admin.user.*`, `bagel.rpc.delegation.*`, `bagel.rpc.internal.{tokens,billing.apply,projection.users.get,users.email.get}`; emits `data.users.*` |
| [Commands](/microservices/commands/) | `app/commands/` | Go | Owns `bagel_commands`: per-broadcaster custom chat commands and lifetime use counters | serves `bagel.rpc.commands.*`, `bagel.rpc.internal.projection.commands.get`; emits `data.commands.changed` |
| [Modules](/microservices/modules/) | `app/modules/` | Go | Owns `bagel_modules`: module toggles and configs, the quote book, the feed counter, sealed Govee keys | serves `bagel.rpc.modules.*`, `bagel.rpc.internal.projection.modules.get`, `bagel.rpc.internal.govee.key.*`; emits `data.modules.changed` |
| [Loyalty](/microservices/loyalty/) | `app/loyalty/` | Go | Owns `bagel_loyalty`: per-viewer points, watch time, and named counters, folded from a firehose of accruals | serves `bagel.rpc.loyalty.*`; consumes `data.loyalty.*`, `data.users.deleted` |
| [Transactions](/microservices/transactions/) | `app/transactions/` | Go | Owns `bagel_transactions`: Tebex checkout baskets and a webhook audit log; applies entitlements through users | serves `bagel.rpc.transactions.basket_create`; terminates the Tebex webhook (HTTPS); calls `bagel.rpc.internal.billing.apply` |
| [Notifications](/microservices/notifications/) | `app/notifications/` | Go | Owns `bagel_notifications`: dashboard notifications, admin announcements, tiered per-user expiry, a cron janitor | serves `bagel.rpc.notifications.*`, `bagel.rpc.admin.notifications.*` |
| [Gateway](/microservices/gateway/) | `app/gateway/` | Go | The fleet's single door to third-party HTTP APIs, cached and rate-limited behind RPC; RPC-only (no stream) | serves `bagel.rpc.gateway.*` |
| [Console](/microservices/console/) | `console/` | SvelteKit SSR | `dashboard` (broadcaster self-serve) and `admin` (operator); arctic OAuth; holds no application data of its own | client only; reaches every service over NATS RPC |
| [Web](/microservices/web/) | `web/` | Astro | Public marketing site, prerendered and served from Cloudflare Pages; off-cluster, no NATS | none (static, no cluster presence) |

See [System state](/reference/system-overview/) for the end-to-end request flow, [RPC contracts](/reference/rpc-contracts/)
for the full request-reply surface, and the [architecture overview](/architecture/) for the views and quality
tactics that shape the fleet.

## Service-to-service shape

Two traffic classes on NATS, carried on two connections per runtime (per-account isolation, below):

- **Events** (fire-and-forget, JetStream or core pub/sub). Domain change events `data.*` (emitted by the owning
  data service, consumed by the projector and peer caches), ingress lanes `twitch.ingress.event.*`, shard and authz
  status `twitch.ingress.status.*`, outgress action lanes `twitch.outgress.*`, and the scope-per-subject cache
  invalidation family `bagel.cache.invalidate.<scope>`.
- **RPC** (request-reply). Everything under `bagel.rpc.*` plus the ingress shard control subjects
  `twitch.ingress.admin.shards.*`. Each business handler subscribes in a **queue group**, so any replica can answer
  and load spreads across the fleet. `pkg/bus` normalizes the conventional `{"error": ""}` reply into a Go error, so
  a failed reply is never mistaken for a zero-valued success.

A data service is the **only writer** of its schema. Any other service that needs that data asks for it over RPC or
learns it from a `data.*` event; it never opens the database. Settings writes are write-behind
([ADR 0008](/adr/0008-caching-and-write-behind-strategy/)): the caller gets an optimistic reply, the row lands on a
flush window, a full-state `data.*` event reconverges the projection, and a `bagel.cache.invalidate.<scope>` publish
evicts the exact cached entry that changed. The money and identity paths (tier, tokens, Tebex records, config
compare-and-swap) never touch the batcher.

## Inter-service authentication

There is no service mesh: Linkerd was removed fleet-wide. Wire encryption is **NATS-native TLS** verified against
the fleet CA (and Valkey native TLS on 6380); management and SSH ride the Tailscale tailnet, while pod and service
data ride a kernel WireGuard mesh (see [Networking](/infrastructure/networking/)).

- **Account isolation, not just subject ACLs.** The bus is split into account boundaries so a compromised credential
  reaches only the subjects its account explicitly imports. Every runtime holds two connections: a shared **BUS**
  account for the JetStream event plane (dialed straight to the hub), and a per-service **`<SERVICE>_RPC`** account
  for the request-reply and cache-invalidation plane (kept on the node-local leaf). A service answers only the RPCs
  it exports and can issue only the RPCs it imports.
- **Secrets travel on export-gated internal subjects.** Plaintext Twitch tokens transit only
  `bagel.rpc.internal.tokens.*` (exported by `USERS_RPC`, imported by `OUTGRESS_RPC` alone); the decrypted Govee key
  transits only `bagel.rpc.internal.govee.key.*` (exported by `MODULES_RPC`, imported by `GATEWAY_RPC` alone); the
  billing-apply, contact-email, and projection reads are gated the same way. No other account can subscribe to them.
- **The console is a client, not a peer.** The broadcaster dashboard runs under `DASHBOARD_RPC` and the operator
  admin under `ADMIN_RPC`; both may import RPCs but export nothing, so neither can ever answer another service's
  request. The admin surface is additionally reachable only over the tailnet, with a DB-backed staff roster as the
  identity boundary on top of the network boundary.
- **OAuth.** The dashboard authenticates end users with arctic (Twitch OAuth); broadcaster grants are sealed and
  persisted by the users service via `grant_save`.
