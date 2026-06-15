---
title: Service registry
description: The services that make up ItsBagelBot, what they own, how they talk, and how they authenticate to each other.
---

Every service is independently deployable for zero-downtime rollouts. Go for the
data and worker services, Elixir/OTP for the Twitch ingress, SvelteKit (SSR) for
the console. The only inter-service transport is **NATS**: subject-based pub/sub
for events, request-reply for RPC. No service reads another service's database.

## Registry

| Service | Repo path | Language | Owns / does | Exposes |
|---|---|---|---|---|
| [Twitch Ingress](/microservices/twitch-ingress/) | `app/ingress/` | Elixir (OTP 27+) | EventSub Conduit + WebSocket shards; per-shard supervision; tenant OAuth; filter-and-normalize events | `twitch.ingress.event.*`, `twitch.ingress.status.*`, `twitch.ingress.admin.shards.get` |
| **Outgress** | `app/outgress/` | Go | Sends to Twitch; per-broadcaster rate limit (Valkey); channel registry; app/user token lifecycle; kill switch | consumes `twitch.outgress.*`; serves `bagel.rpc.outgress.*` |
| **Projector** | `app/projector/` | Go | Builds the Valkey settings projection on stream-online; serves broadcaster tier lookups | `bagel.rpc.broadcaster.status.get` |
| **Users** | `app/users/` | Go | User accounts, status (free/paid/vip), active toggle, Twitch OAuth tokens | `bagel.rpc.dashboard.*`, `bagel.rpc.admin.user.*`, `bagel.rpc.internal.projection.users.get`, `bagel.rpc.internal.tokens.*`; emits `data.users.*` |
| **Commands** | `app/commands/` | Go | Custom chat commands | `bagel.rpc.commands.*`, `bagel.rpc.internal.projection.commands.get`; emits `data.commands.changed` |
| **Modules** | `app/modules/` | Go | Per-broadcaster feature modules | `bagel.rpc.internal.projection.modules.get`; emits `data.modules.changed` |
| **Transactions** | `app/transactions/` | Go | Tebex purchase records | consumes `data.transactions.recorded` |
| [Admin](/microservices/admin/) (legacy) | `app/admin/` | Go + templ | Read-only operator window over NATS (shard fleet, users) | Operators only, over the tailnet |
| **Console** | `console/` | SvelteKit SSR | `dashboard` (broadcaster self-serve) + `admin` (operator); arctic OAuth | HTTPS via cloudflared / tailnet; talks to services only over NATS RPC |

See [System state](/reference/system-overview/) for the data plane, the bus, and
the end-to-end request flow, and [RPC contracts](/reference/rpc-contracts/) for the
full request-reply surface.

## Service-to-service shape

Two traffic classes on NATS:

- **Events** (fire-and-forget pub/sub). Domain change events `data.*` (emitted by
  the owning data service, consumed by projector and peers), ingress events
  `twitch.ingress.event.*`, shard status `twitch.ingress.status.*`, outgress send
  lanes `twitch.outgress.*`, and the cache invalidation broadcast
  `bagel.cache.invalidate.broadcaster`.
- **RPC** (request-reply). Everything under `bagel.rpc.*` plus the ingress shard
  snapshot `twitch.ingress.admin.shards.get`. Each handler subscribes in a **queue
  group**, so any replica can answer and load spreads across the fleet.

A data service is the **only writer** of its schema. Any other service that needs
that data asks for it over RPC; it never opens the database. Writes are
write-behind: the caller gets an optimistic reply, the DB updates asynchronously,
and a `bagel.cache.invalidate.broadcaster` publish reconverges cached readers.

## Inter-service authentication

- **Transport perimeter**: services run inside the cluster on the tailnet; there is
  no public NATS listener. Public ingress is the outbound cloudflared tunnel only.
- **NATS authorization**: subjects carrying secrets (the bot-account token verbs
  under `bagel.rpc.internal.tokens.*`) are restricted by NATS account/permission
  so only outgress and users may use them. Plaintext tokens never transit any
  other subject.
- **Mesh**: Linkerd native-sidecar provides mTLS between meshed services. The
  legacy admin tool is intentionally **not** meshed and is reachable only over
  Tailscale, with tailnet ACLs as its access control (see [Networking](/infrastructure/networking/)).
- **OAuth**: the console authenticates end users with arctic (Twitch OAuth);
  broadcaster grants are persisted by the users service via `grant_save`.
