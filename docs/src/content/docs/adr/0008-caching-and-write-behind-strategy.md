---
title: "0008 - Caching and Write-Behind Strategy"
description: "Architecture decision record: In-process caching with stampede protection,
write-behind batching, and event-carried invalidation over NATS"
---

**Date:** 2026-06-09

## Status

Accepted

## Context

The data services sit on the free HeatWave instance from
[ADR 0005](/adr/0005-adoption-of-mysql-heatwave/): 8 GB of RAM shared by every schema in the system. Two access
patterns threaten it.

On the read side, the bot's hot path is chat. A popular channel can ask "what are this user's modules?" hundreds of
times a minute, and the answer changes rarely. Without a cache every chat message becomes a query; with a naive
cache every expiry becomes a stampede, because all the requests that miss at the same instant race to run the same
query. Entries written together also expire together, which turns one popular channel into a synchronized thundering
herd.

On the write side, settings are edited from a dashboard. A streamer flipping a toggle five times in two seconds, or
iterating on a command's wording, would produce one autocommit write per click. Multiplied across tenants, the
database spends its capacity persisting states that were obsolete before the transaction committed.

There is a third force: services scale horizontally. An in-process cache on instance A knows nothing about a write
that went through instance B, so invalidation has to travel between instances, and
[ADR 0003](/adr/0003-adoption-of-nats-as-communication-bridge/) already gives us the bus to carry it.

Laying down our requirements:

- Reads served from process memory, with a hard guarantee that N concurrent misses on one key cost one query.
- No synchronized expiry across entries.
- Writes to the same key within a short window must collapse into one row write, and a burst of writes must land as
  one transaction, not as N round trips.
- The money path (Tebex transactions, tier changes) and tokens must never sit in a write-behind buffer.
- Invalidation must reach every instance of a service, and the projector must see every change exactly once.
- Consumers must tolerate redelivery, because the bus is at-least-once.

## Decision

**In-process cache with stampede protection (`pkg/cache`).** A sharded TTL map (16 shards, lock per shard, FNV-1a
key hashing without allocation). Misses go through `singleflight`: any number of concurrent misses on one key
collapse into a single loader call, and the waiters share the result. TTLs carry a random jitter of up to 10% so
entries never expire in unison. Errors are never cached. Invalidation also forgets any in-flight load for the key,
so a stale flight cannot repopulate the cache after the fact.

**Write-behind batching for settings (`pkg/batch`).** Module and command writes go through a coalescing batcher:
writes to the same key within a flush window (2 seconds, or 256 pending keys, whichever comes first) collapse into
the latest value, and the whole window lands in a single database transaction. A failed flush is retried on the next
window without clobbering newer writes. Five clicks on the same toggle cost one row write. The trade-off is stated
on the type itself: a value sits in memory for at most the flush interval before it is persisted, so only state that
a user can re-submit goes through it. Transactions, tier changes, and tokens write through immediately.

**Event-carried invalidation over Watermill (`pkg/bus`).** Events publish only after the database commit and carry
the full new state, so consumers update themselves from the event alone and never read another service's schema.
We use Watermill over the JetStream cluster from [ADR 0003](/adr/0003-adoption-of-nats-as-communication-bridge/)
rather than raw `nats.go`, because it gives us the publisher/subscriber abstraction, ack/nack semantics, and test
doubles for the cost of one thin dependency. Two subscription shapes, on purpose:

- Cache invalidation is a broadcast: no queue group, so every instance of a service drops its cached keys when any
  instance writes.
- The projector consumes through a durable queue group, so each event is folded into the projection exactly once
  and the consumer keeps its position across restarts.

**Poison messages are dropped, not nacked.** JetStream redelivers unacknowledged messages without limit by default,
so a malformed payload that nacks forever would stall the stream. Consumers validate every payload; what fails
validation or decoding is logged and acknowledged away.

## Consequences

- Reads have a bounded staleness of the cache TTL (5 minutes) in the worst case, and far less in practice because
  change events invalidate ahead of expiry. A service that needs read-your-own-write semantics invalidates its local
  cache synchronously on write, which the repositories already do.
- Settings writes can be lost in a window of at most the flush interval if the process dies. We accept this for
  toggles and command edits, which the user can re-submit, and we explicitly route money and tokens around the
  batcher. Shutdown flushes the pending window.
- Every consumer must stay idempotent. The event payloads are full-state overwrites, so redelivery is naturally
  harmless, and that property has to be preserved as the contracts evolve.
- The event contracts in `internal/domain/event/data` are now part of the public surface, with the same versioning
  care the ADR 0003 subjects demand.
- Memory per instance grows with the working set of cached views. The caches store small view structs, never
  tokens, and the background sweeper reclaims expired entries.

## Alternatives considered

- **Read-through Valkey for everything.** One shared cache would make invalidation trivial, but it puts a network
  round trip on every hot-path read, which defeats the purpose of in-process caching. Valkey has a different job in
  this system: the cross-service settings projection
  (see [ADR 0009](/adr/0009-adoption-of-valkey-for-the-settings-projection/)).
- **Write every change immediately.** Simple and durable, but it is exactly the per-click hammering the database
  should not absorb, and the dashboard would be rate-limited by MySQL round trips.
- **Debounce in the frontend instead.** Helps the dashboard case but protects nothing else; any API consumer or
  future ingress can still write per event. The boundary that owns the data has to own the protection.
- **groupcache or a distributed in-process cache.** Solves stampedes across instances, but brings peer discovery
  and an HTTP mesh between instances, which contradicts the substrate decision in
  [ADR 0003](/adr/0003-adoption-of-nats-as-communication-bridge/). Singleflight per instance plus event
  invalidation covers the same risk with no new infrastructure.
- **Raw `nats.go` instead of Watermill.** One dependency fewer, but we would reimplement subscriber lifecycles,
  ack handling, and test publishers ourselves. The library is thin enough that the trade goes the other way.
