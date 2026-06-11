---
title: "0009 - Adoption of Valkey for the Settings Projection"
description: "Architecture decision record: Adoption of Valkey as the read-side projection
of all settings and tier status, fed by a dedicated projector service"
---

**Date:** 2026-06-09

## Status

Accepted

## Context

The hot path of the whole system is reacting to a chat message. To do that, an ingress worker (see
[ADR 0006](/adr/0006-adoption-of-elixir-for-twitch-ingress/)) needs the channel's settings: which modules are
enabled, with what configuration, and what tier the streamer is on. That read happens for potentially every message
on every channel, and it cannot touch MySQL: the free HeatWave instance from
[ADR 0005](/adr/0005-adoption-of-mysql-heatwave/) is sized for state of record, not for per-message reads.

The in-process caches from [ADR 0008](/adr/0008-caching-and-write-behind-strategy/) protect each data service from
its own readers, but they do not help a consumer in another process, let alone one written in another language. The
ingress workers should not each maintain their own cache hierarchy against four services, re-implementing
invalidation in Elixir.

What the read side wants is one place where the full settings of a user can be fetched in a single round trip, kept
fresh by the system rather than by the reader.

Laying down our requirements:

- One key-value read returns everything the hot path needs for a channel: tier status, active flag, every module
  toggle, every module config.
- Kept up to date by change events, without the readers knowing where the data lives.
- Rebuildable from scratch: the projection is a cache, never the system of record.
- Open source, low footprint, with mature clients in both Go and Elixir.
- No reads against the data services' schemas, which would break the isolation from
  [ADR 0005](/adr/0005-adoption-of-mysql-heatwave/).

## Decision

We adopt **Valkey** as the settings projection store, written to by a dedicated **projector** service
(`app/projector`) and read by anything on the hot path.

**Layout.** One hash per user, readable with a single `HGETALL`:

```
settings:<user_id>
  status                  free | paid | vip
  active                  0 | 1
  module:<name>:enabled   0 | 1
  module:<name>:config    raw JSON
```

Readers parse nothing except the config blob of the module they actually use. Module names are strictly validated
at the write boundary (lowercase alphanumerics, underscore, hyphen), because the name is embedded in the field; a
colon in a name could otherwise forge another field.

**The projector is a pure event consumer.** It subscribes to the user and module change events through a durable
queue group, so every event is folded into Valkey exactly once and the consumer keeps its position across restarts.
Every handler is an overwrite of the full state carried by the event, which makes redelivery and replay harmless.
The projector never queries another service's schema.

**Cold starts use a reproject handshake.** On startup the projector publishes `data.reproject.request`. Each data
service answers from its own durable group (so exactly one instance per service replays), republishing its current
rows as ordinary change events, paged so a table is never loaded at once. The projection of a fresh or wiped Valkey
converges without anyone crossing a schema boundary, and because writes are overwrites, a replay racing live
changes settles on the latest state.

**Why Valkey over Redis.** Same protocol, same data model, same clients; the difference is governance and license.
Valkey is the Linux Foundation fork that stayed BSD after Redis moved to restrictive licensing, which matters for a
project that may be self-hosted on the Oracle ARM node indefinitely.

## Consequences

- The hot path reads one hash from Valkey and never touches MySQL. The database serves writes and cold rebuilds.
- The projection is eventually consistent. A settings change is visible after the write-behind window from
  [ADR 0008](/adr/0008-caching-and-write-behind-strategy/) plus the event hop, which is well inside what a dashboard
  user perceives as immediate, but it is not read-your-write for an external reader.
- Valkey becomes part of the hot path's availability story. Losing it does not lose data (the projection rebuilds
  from a reproject request), but readers must degrade deliberately while it is gone.
- An event missed beyond the JetStream retention window leaves the projection stale for the affected user until the
  next change or the next reproject. We accept this per the retention posture of
  [ADR 0003](/adr/0003-adoption-of-nats-as-communication-bridge/), and the reproject handshake doubles as the
  reconciliation tool.
- Commands are deliberately not projected. They are read by the commands service itself on a slower path and served
  from its in-process cache; projecting them would grow every hash for data the hot path does not need on every
  message.

## Alternatives considered

- **Redis.** Functionally identical today, but the license direction is wrong for this project's self-hosting
  posture, and Valkey carries the open governance. Rejected on principle, at zero technical cost.
- **NATS JetStream KV.** Already running, one less system. But the projection wants hash-per-user with field-level
  writes and a single-read fetch of many fields, which KV's flat key model does not give us without inventing an
  encoding, and per-message reads belong on a data structure server, not on the coordination substrate. Rejected.
- **Each consumer caches against the services.** No new infrastructure, but every consumer (including the Elixir
  ingress) re-implements caching and invalidation, and the data services absorb every consumer's cold misses. The
  projector centralizes that work once, in one place. Rejected.
- **Request/reply to the services on the hot path.** Correct and always fresh, but it puts four services and the
  database inside the latency budget of every chat message. That is the exact coupling the projection exists to
  remove. Rejected.
