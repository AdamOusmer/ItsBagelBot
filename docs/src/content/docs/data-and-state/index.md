---
title: Data & state
description: Where data lives, who owns it, and how state crosses service boundaries in ItsBagelBot.
---

This section documents the stores ItsBagelBot relies on for persistent data, the state each service is allowed to
keep, and the rules that govern data crossing service boundaries.

Three categories of state live in the system, kept deliberately separate so each can fail, restart, and be reasoned
about independently:

- **Persistent state.** Long-lived data behind the product: tenant records, broadcaster configuration, OAuth grants,
  command definitions, audit history. Lives in MySQL HeatWave under per-service schemas.
  See [ADR 0005](/adr/0005-adoption-of-mysql-heatwave/).
- **Ephemeral state.** Per-shard, per-connection state held in service memory. Lost on restart by design. The
  supervisor's job is to make that restart safe and bounded, not to persist the state behind it. Twitch Ingress shard
  state is the canonical example.
- **In-flight state.** Events traversing the NATS56 bus between producers and consumers. Not a store, but a transport
  whose delivery semantics the ownership rules below depend on.
  See [ADR 0003](/adr/0003-adoption-of-nats-as-communication-bridge/).

## Store registry

| Store                          | Class                 | Owner          | Holds                                       | Replication                  | Backup                                  |
|--------------------------------|-----------------------|----------------|---------------------------------------------|------------------------------|-----------------------------------------|
| MySQL HeatWave (Always Free)   | Relational, managed   | OCI, Montreal  | All persistent state, per-service schema    | Storage-layer HA (provider)  | Off-provider logical dump (cadence TBD) |

> In Progress: any additional store (cache, object storage, JetStream-backed stream) as it is adopted. Each new
> store gets a row here and a section below describing its ownership, lifecycle, and recovery posture.

## Ownership rules

Per-service schema isolation is the load-bearing rule and follows directly from
[ADR 0001](/adr/0001-rewriting-to-microservices/) and [ADR 0005](/adr/0005-adoption-of-mysql-heatwave/).

- A service owns exactly one MySQL schema. Other services do not read or write to it directly.
- Cross-service data flows through the documented API or event surface, never through cross-schema joins.
- Foreign keys do not cross schema boundaries. A reference to another service's entity is an opaque ID resolved at
  the API layer.

> In Progress: write paths versus read paths, who is allowed to issue migrations, how to handle data that two
> services both legitimately need.

## Multi-tenancy

> In Progress: tenant scoping at the row level, the column convention every persistent table is expected to carry,
> and how a tenant deletion cascades across services that hold a copy of its ID.

## Backup and restore

HeatWave provides storage-layer HA, which protects against hardware loss but **not** against operator error or
account loss. Logical backups are taken off-provider so that a worst-case account incident is recoverable.

> In Progress: backup cadence, retention window, off-provider destination, and the restore drill schedule that
> proves the backups are actually restorable.

## Schema conventions

To keep a future move to managed Postgres a migration rather than a rewrite, schemas stay inside the common SQL
subset. See the consequences section of [ADR 0005](/adr/0005-adoption-of-mysql-heatwave/) for the constraints this
implies.

> In Progress: charset and collation, isolation level, SQL mode, timestamp handling, identifier naming, and the
> short list of MySQL-only syntax we deliberately avoid.

## What does *not* live in a shared store

- **Per-shard runtime state.** Held in service memory, rebuilt on restart. A service that needs cross-process
  visibility of its own runtime state should make that visibility explicit (an API, an event), not coerce a shared
  store into the role.
- **Cross-service joinable data.** If two services find themselves wanting a SQL join, the answer is an API or an
  event, not a third schema that bridges them.
- **Secrets.** OAuth grants stored in MySQL are encrypted at rest at the application layer before they reach the
  schema. Provider-level encryption is treated as defense in depth, not as the primary control.
