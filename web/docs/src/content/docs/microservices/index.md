---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Service registry
description: The services that make up ItsBagelBot, what they own, how they talk, and how they authenticate to each other.
---

Every service is independently deployable for zero-downtime rollouts. Go for the
data and worker services, Elixir/OTP for the Twitch ingress, SvelteKit (SSR) for
the dashboard and admin consoles. The only inter-service transport is **NATS**: subject-based pub/sub
for events, request-reply for RPC. No service reads another service's database.

## Registry

| Service | Repo path | Language | Owns / does | Exposes |
|---|---|---|---|---|
| [Twitch Ingress](/microservices/twitch-ingress/) | `app/twitch/ingress/` | Elixir (OTP 27+) | EventSub Conduit + WebSocket shards; per-shard supervision; filter, squash and normalize events | `twitch.ingress.event.*`, `twitch.ingress.status.*`, `twitch.ingress.admin.*`, `bagel.rpc.ingress.conduit.get` |
| [Sesame](/microservices/sesame/) | `app/twitch/sesame/` | Go | Core engine and command processor; consumes ingress events, runs incoming moderation, module handlers and commands, routes to outgress | consumes `twitch.ingress.event.*`; publishes to `twitch.outgress.*` |
| [Outgress](/microservices/outgress/) | `app/twitch/outgress/` | Go | Twitch API and chat delivery; fleet-coordinated rate limits (Valkey); channel registry and pause switch; token use and refresh; EventSub enrollment | consumes `twitch.outgress.*`; serves `bagel.rpc.outgress.*` |
| [Discord Ingress](/microservices/dingress/) (`dingress`) | `app/discord/ingress/` | Go | Discord gateway session for the fleet bot token (Valkey lease, one active replica); relays gateway events; defers interactions | publishes `discord.ingress.event.*` |
| Discord Engine | `app/discord/engine/` | Go | Decides Discord community behavior (welcomes, tickets, voice rooms, logs, link guard, live posts) from gateway and Twitch events | consumes `discord.ingress.event.*`; publishes `discord.outgress.*` |
| Discord Outgress | `app/discord/outgress/` | Go | Executes Discord REST actions; guild setup and layout; slash-command registration | consumes `discord.outgress.*`; serves `bagel.rpc.dingress.*`, `bagel.rpc.discord-outgress.*` |
| [Projector](/microservices/projector/) | `app/projector/` | Go | Folds owner events into Valkey read models; serves status, live, stream info and dashboard reads; live counters | `bagel.rpc.broadcaster.status.get`, `bagel.rpc.broadcaster.live.get`, `bagel.rpc.projector.dashboard.*` |
| Gossip | `app/gossip/` | Go | External API providers behind typed RPC: caching, upstream budgets, credential lookup, custom fetches | `bagel.rpc.gossip.*` |
| [Users](/microservices/users/) | `app/db/users/` | Go | Accounts, status (free/paid/vip), bot-enable state, Twitch token custody, staff and delegation | `bagel.rpc.dashboard.*`, `bagel.rpc.admin.user.*`, `bagel.rpc.internal.projection.users.get`, `bagel.rpc.internal.tokens.*`; emits `data.users.*` |
| [Commands](/microservices/commands/) | `app/db/commands/` | Go | Custom chat commands, use counts, custom-fetch definitions and sealed keys | `bagel.rpc.commands.*`, `bagel.rpc.internal.projection.commands.get`; emits `data.commands.*` |
| [Modules](/microservices/modules/) | `app/db/modules/` | Go | Per-broadcaster module settings, quotes, feed tallies, Govee and Spotify credentials | `bagel.rpc.modules.*`, `bagel.rpc.internal.projection.modules.get`; emits `data.modules.changed` |
| Loyalty | `app/db/loyalty/` | Go | Viewer points, watch time, channel counters | `bagel.rpc.loyalty.*`; consumes `data.loyalty.*` |
| Discord data (`discord-data`) | `app/db/discord/` | Go | Guild bindings, per-guild settings, tickets, member XP | `bagel.rpc.discord-data.*` |
| [Notifications](/microservices/notifications/) | `app/db/notifications/` | Go | Dashboard notifications, admin announcements, and TTL cleanup | `bagel.rpc.notifications.*`, `bagel.rpc.admin.notifications.*` |
| [Transactions](/microservices/transactions/) | `app/db/transactions/` | Go | Tebex webhook audit, checkout baskets, giveaway draws and awards | `bagel.rpc.transactions.*`; HTTPS `/tebex` webhook |
| [Admin](/microservices/admin/) | `web/admin/` | SvelteKit SSR | Staff console: accounts, shard fleet, lanes, giveaways, trials, notifications, deploys | HTTPS over the tailnet only; talks to services over NATS RPC |
| [Console](/microservices/console/) | `web/dashboard/` | SvelteKit SSR | Broadcaster dashboard and public commands pages; Twitch OAuth | HTTPS via Traefik; talks to services over NATS RPC |
| [Web](/microservices/web/) | `web/marketing/` | Astro | Public marketing site (documentation lives in `web/docs/`) | Public HTTPS |

Directories `app/twitch/automod/`, `app/yt-ingress/` and `app/plain/` hold no running service. Incoming AutoMod
runs inside Sesame.

See [System state](/reference/system-overview/) for the data plane, the bus, and
the end-to-end request flow, and [RPC contracts](/reference/rpc-contracts/) for the
full request-reply surface.

## Service-to-service shape

Two traffic classes on NATS:

- **Events** (fire-and-forget pub/sub). Domain change events `data.*` (emitted by
  the owning data service, consumed by projector and peers), ingress events
  `twitch.ingress.event.*`, shard status `twitch.ingress.status.*`, outgress send
  lanes `twitch.outgress.*`, the Discord lanes `discord.ingress.event.*` and `discord.outgress.*`, and the cache
  invalidation broadcasts `bagel.cache.invalidate.<scope>` (for example `status`, `modules`, `commands`, `live`).
- **RPC** (request-reply). Everything under `bagel.rpc.*` plus the ingress shard
  snapshot `twitch.ingress.admin.shards.get`. Each handler subscribes in a **queue
  group**, so any replica can answer and load spreads across the fleet.

A data service is the **only writer** of its schema. Any other service that needs
that data asks for it over RPC; it never opens the database. Many
writes are write-behind: the caller gets an optimistic reply, the DB updates asynchronously,
and a `bagel.cache.invalidate.<scope>` publish reconverges cached readers. Interactive saves
(for example a module upsert from the dashboard) persist before replying.

## Inter-service authentication

- **Transport perimeter**: services run inside the cluster on the tailnet; there is
  no public NATS listener. Public ingress is the outbound cloudflared tunnel only.
- **NATS authorization**: subjects carrying secrets (the bot-account token verbs
  under `bagel.rpc.internal.tokens.*`) are restricted by NATS account/permission
  so only Twitch Outgress and Users may use them. Plaintext tokens never transit any
  other subject.
- **Mutual TLS and accounts**: the NATS server requires a client certificate from every workload (`verify: true`), and
  JWT-based NATS accounts decide which subjects each service may publish, serve or import
  (`deploy/messaging/accounts.yaml`). The admin console is exposed over Tailscale only (see
  [Networking](/infrastructure/networking/)).
- **OAuth**: the dashboard authenticates end users with Twitch OAuth;
  broadcaster grants are persisted by the users service via `grant_save`.
