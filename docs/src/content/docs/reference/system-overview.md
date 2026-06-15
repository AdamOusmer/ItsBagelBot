---
title: System state
description: The current shape of ItsBagelBot. Services, the data plane, the message bus, and how a chat message becomes a reply.
---

This page documents the system **as it runs today**, after the move to per-schema
data microservices, the NATS bus, the Elixir ingress, and the SvelteKit console.
Where older pages still say "RabbitMQ" or `services/twitch-ingress/`, this page is
the source of truth.

## At a glance

- **Microservices**, one concern each, independently deployable for zero-downtime rollouts.
- **Go** for data and worker services, **Elixir/OTP** for the Twitch ingress, **SvelteKit (SSR)** for the operator/user console.
- **NATS** is the only inter-service transport: subject-based pub/sub for events, request-reply for RPC. No service reads another service's database.
- **MySQL HeatWave**, one schema per data service, accessed through `ent`.
- **Valkey** holds the settings/tier projection plus short-lived caches.
- Hosted on **Oracle Cloud (PAYG, Canada)**, two k3s nodes, delivered by **Flux GitOps** behind a **cloudflared** tunnel on a **Tailscale** tailnet.

## Services

| Service | Path | Language | Owns / does |
|---|---|---|---|
| **ingress** | `app/ingress/` | Elixir | Twitch EventSub Conduit + WebSocket shards; per-shard supervision; tenant OAuth; normalizes and publishes events on `twitch.ingress.event.*`; exposes shard snapshot RPC |
| **outgress** | `app/outgress/` | Go | Sends to Twitch (chat + EventSub mgmt); per-broadcaster rate limit via Valkey; channel registry; app/user token lifecycle; kill switch |
| **projector** | `app/projector/` | Go | Builds the Valkey settings projection on stream-online; serves broadcaster billing **tier** lookups |
| **users** | `app/users/` | Go | User accounts, status (free/paid/vip), active toggle, Twitch OAuth tokens; emits `data.users.*` |
| **commands** | `app/commands/` | Go | Custom chat commands; emits `data.commands.changed` |
| **modules** | `app/modules/` | Go | Feature modules per broadcaster; emits `data.modules.changed` |
| **transactions** | `app/transactions/` | Go | Tebex purchase records; consumes `data.transactions.recorded` |
| **admin** (legacy) | `app/admin/` | Go + templ | Read-only operator window over NATS (shard fleet, users); being superseded by the console |
| **console** | `console/` | SvelteKit SSR | `dashboard` (broadcaster self-serve) and `admin` (operator) apps; arctic OAuth; talks to services only over NATS RPC |

## Data plane

- **Database**: MySQL HeatWave. Each data service owns its own schema and is the
  only writer. Cross-service reads go over NATS RPC, never SQL.
- **ORM**: `ent`, generated code per service under `app/<svc>/ent/`.
- **Projection**: Valkey stores the per-broadcaster settings/tier projection the
  hot path reads. Writes are **write-behind** (~2s): the dashboard returns an
  optimistic view, the database is updated asynchronously, and a cache
  invalidation event reconverges readers.
- **Caching**: short-lived in-process LRU caches in front of Valkey (e.g. the
  projector tier cache, 30s TTL) absorb repeat lookups.

## Message bus

NATS carries two traffic classes:

- **Events** (fire-and-forget pub/sub). Domain change events `data.*`, ingress
  events `twitch.ingress.event.*`, ingress shard status `twitch.ingress.status.*`,
  outgress send lanes `twitch.outgress.*`, and the cache invalidation broadcast
  `bagel.cache.invalidate.broadcaster`.
- **RPC** (request-reply). Everything under `bagel.rpc.*` plus the ingress shard
  snapshot `twitch.ingress.admin.shards.get`. See
  [RPC contracts →](/reference/rpc-contracts/).

### Premium / standard lanes

Outgress traffic is split by broadcaster status. Premium-tier broadcasters (paid
or vip), plus a configured set of always-premium special user IDs, route through
`twitch.outgress.premium`; everyone else through `twitch.outgress.standard`.
System messages use `twitch.outgress.system`. Non-command chat from
non-special, non-premium users is dropped before it reaches a lane.

## Request flow (chat command)

1. Twitch delivers a chat event to an **ingress** shard over the EventSub Conduit WebSocket.
2. Ingress normalizes it and publishes on `twitch.ingress.event.{premium|standard}`.
3. The owning worker consumes the lane, resolves the command via the **commands**
   projection (or RPC), and produces a reply.
4. The reply is published on `twitch.outgress.{premium|standard|system}`.
5. **outgress** applies the per-broadcaster rate limit and sends to Twitch using
   the bot account token (refreshed and rotated through the users token RPC).

## Infrastructure

- **Cloud**: Oracle Cloud Infrastructure, pay-as-you-go, Canadian region.
- **Cluster**: two k3s nodes. `node1` is ARM (`opc@`), `node2` is Intel (`ubt@`).
  Images are built natively per node and imported into k3s with `ctr`; no cross-arch emulation.
- **Network**: nodes have no public IPs. All traffic is on the Tailscale tailnet;
  public ingress is the outbound **cloudflared** tunnel. Traefik and cloudflared
  run as per-node DaemonSets with closest-node routing.
- **Mesh**: Linkerd native-sidecar.
- **Delivery**: pull-based **Flux CD** from GHCR with digest-pinned images. The
  old root SSH build/deploy scripts are deprecated.
- **NATS config**: `.conf` files are managed out-of-band via `apply-nats.sh`
  (not Flux), hot-reloaded with `SIGHUP`.

## Targets

- Console SSR p99 latency: ≤ 200 ms.
- Rollouts are zero-downtime; on the 2-node cluster, rolling updates patch
  `maxSurge=0 / maxUnavailable=1` to avoid the hard pod anti-affinity deadlock,
  and images are imported on both nodes before a digest bump.
