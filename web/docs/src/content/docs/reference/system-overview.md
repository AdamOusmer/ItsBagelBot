---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: System state
description: The current shape of ItsBagelBot. Services, the data plane, the message bus, and how a chat message becomes a reply.
---

This page documents the system **as it runs today**: per-schema data microservices,
the NATS bus, the Elixir ingress, and the SvelteKit consoles. Where an older page
disagrees, this page and the code are the source of truth.

## At a glance

- **Microservices**, one concern each, independently deployable for zero-downtime rollouts.
- **Go** for data and worker services, **Elixir/OTP** for the Twitch ingress, **SvelteKit (SSR)** for the broadcaster and staff consoles.
- **NATS** is the only inter-service transport: subject-based pub/sub and JetStream for events, request-reply for RPC. No service reads another service's database.
- **MySQL HeatWave**, one schema per data service, accessed through `ent`.
- **Valkey** holds the settings and tier projection plus short-lived caches.
- Runs on **k3s** with Cilium, behind a **cloudflared** tunnel, with **Tailscale** for operator access.

## Services

| Service | Path | Language | Owns / does |
|---|---|---|---|
| **ingress** | `app/twitch/ingress/` | Elixir | Twitch EventSub Conduit + WebSocket shards; per-shard supervision; normalizes and publishes events on `twitch.ingress.event.*`; exposes the shard snapshot RPC |
| **sesame** | `app/twitch/sesame/` | Go | Consumes ingress lanes; runs modules, custom commands, timers and incoming AutoMod; publishes typed outgress requests |
| **outgress** | `app/twitch/outgress/` | Go | Executes Twitch actions (chat, EventSub management); per-broadcaster rate limit via Valkey; channel registry; token lifecycle; kill switch |
| **projector** | `app/projector/` | Go | Builds the Valkey projections from owner events; serves tier, live and dashboard reads; live counters and boards |
| **users** | `app/db/users/` | Go | Accounts, status (free/paid/vip), staff roles, Twitch OAuth tokens; emits `data.users.*` |
| **commands** | `app/db/commands/` | Go | Custom chat commands and fetch definitions; emits `data.commands.*` |
| **modules** | `app/db/modules/` | Go | Per-broadcaster module configuration; emits `data.modules.changed` |
| **loyalty** | `app/db/loyalty/` | Go | Viewer balances, watchtime and channel counters |
| **transactions** | `app/db/transactions/` | Go | Tebex billing, giveaways and award delivery |
| **notifications** | `app/db/notifications/` | Go | Account-facing notification records |
| **discord-data** | `app/db/discord/` | Go | Discord guild configuration, tickets and XP |
| **discord** | `app/discord/{ingress,engine,outgress}/` | Go | Discord gateway session, module decisions, and REST effects |
| **gossip** | `app/gossip/` | Go | Gateway for external APIs, with caching and upstream budgets |
| **deployer** | `app/deployer/` | Go | Release and cluster-apply runs driven from the admin console |
| **console** | `web/dashboard/`, `web/admin/` | SvelteKit SSR | `dashboard` (broadcaster self-serve and public pages) and `admin` (staff); talks to services only over NATS RPC |

`CONTEXT-MAP.md` at the repository root lists
placeholders such as `app/plain/` and `app/yt-ingress/`; they are not running services.

## Data plane

- **Database**: MySQL HeatWave. Each data service owns its own schema and is the
  only writer. Cross-service reads go over NATS RPC, never SQL.
- **ORM**: `ent`, generated code per service under `app/db/<svc>/ent/`.
- **Projection**: Valkey stores the per-broadcaster settings projection
  (`settings:<user_id>`) the hot path reads. Commands and modules writes are
  **write-behind** (~2s): the dashboard returns an optimistic view, the database
  is updated asynchronously, and a cache invalidation event reconverges readers.
- **Caching**: short-lived in-process caches in front of Valkey (for example the
  projector tier cache, 30s TTL) absorb repeat lookups.

## Message bus

NATS carries two traffic classes:

- **Events** (pub/sub and JetStream). Domain change events `data.*`, ingress
  events `twitch.ingress.event.*`, ingress shard status `twitch.ingress.status.*`,
  outgress send lanes `twitch.outgress.*`, and the cache invalidation broadcasts
  `bagel.cache.invalidate.<scope>`. The stream catalog is in `pkg/bus/streams.go`.
- **RPC** (request-reply). Everything under `bagel.rpc.*` plus the ingress admin
  subjects `twitch.ingress.admin.*`. See [RPC contracts →](/reference/rpc-contracts/).

The brokers run in JWT operator mode with per-service accounts and roles
(`deploy/messaging/accounts.yaml`), so a service can publish and subscribe only to
the subjects its role grants.

### Premium / standard lanes

Ingress routes each event to a lane. Chat from Premium broadcasters (status paid
or vip), plus a configured set of always-premium special user IDs, takes
`twitch.ingress.event.premium`; everyone else takes `.standard`. Stream online and
offline events also go to `.stream`. Sesame carries the incoming priority through
to its output, so replies leave on `twitch.outgress.premium` or
`twitch.outgress.standard`. Control jobs such as EventSub enrollment use
`twitch.outgress.system`. Unknown broadcasters and lookup failures route as standard.

## Request flow (chat command)

1. Twitch delivers a chat event to an **ingress** shard over the EventSub Conduit WebSocket.
2. Ingress normalizes it and publishes on `twitch.ingress.event.{premium|standard}`.
3. **Sesame** consumes the lane, loads the channel's modules and commands from the
   projection (Valkey, with an owner RPC fallback), runs the gates, and builds a reply.
4. The reply is published on `twitch.outgress.{premium|standard}`.
5. **outgress** applies the per-broadcaster rate limit and sends via Helix.
   Twitch associates `sender_id` with the bot's `user:bot` grant and the
   broadcaster's `channel:bot` grant, which makes eligible replies display the Chat Bot badge.

## Infrastructure

- **Cluster**: k3s with one tainted ARM control-plane node (`cp1`) and three worker
  nodes labelled `role=node`. Images are built natively for ARM and Intel and pulled from GHCR.
  See [Hardware & cluster →](/infrastructure/hardware-and-cluster/).
- **Network**: public traffic enters through the outbound **cloudflared** tunnel to
  Traefik. Operators reach the admin console, NATS monitoring and the Kubernetes API over
  Tailscale. Cilium provides the CNI, and each namespace starts from default-deny policies.
  See [Networking →](/infrastructure/networking/).
- **Delivery**: GitHub Actions builds images and the deployer pins them by digest in
  `deploy/k8s` (a CI test enforces the digest pins). Flux no longer runs on the cluster.
- **NATS**: three hub members hold every JetStream stream at R3, plus a leaf
  DaemonSet that services connect to (`deploy/messaging/README.md`).

## Targets

- Rollouts are zero-downtime. Because one-per-node anti-affinity leaves no free node
  for a surge pod, most Deployments set `maxSurge: 0` and `maxUnavailable: 1`
  (`deploy/k8s/*.yaml`).
