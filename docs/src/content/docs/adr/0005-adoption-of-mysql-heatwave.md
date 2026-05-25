---
title: "0005 - Adoption of MySQL HeatWave"
description: "Architecture decision record: Adoption of MySQL HeatWave as the relational database"
---

**Date:** 2026-05-23

## Status

Accepted

## Context

The system needs a relational database for the parts of the state that do not belong in a key-value store: user
records, tenant configuration, OAuth tokens, command definitions, anything where rows, foreign keys, and ad-hoc
queries are the natural fit. The earlier plan for this slot was PostgreSQL, which had been the default choice for
the v1.

Postgres is, on paper, the better engine. Its SQL is closer to the standard, its planner is more capable on
non-trivial joins, its extension ecosystem (PostGIS, pg_trgm, pgvector, partial and expression indexes, rich JSONB
operators, robust CTEs and window functions) is wider than MySQL's, and its default semantics (transactional DDL,
stricter type handling, sane isolation behavior) tend to surprise developers less. If the only criterion were
"which engine is the most capable for a small team," Postgres would win.

That is not the only criterion. The constraints from
[ADR 0004](/adr/0004-adoption-of-oracle-cloud/) apply directly here:
the project runs on a student budget, on Oracle Cloud Always Free capacity, in Montreal. Two real options follow
from that posture and one does not:

- A managed Postgres instance comparable to what the system needs is not part of the OCI Always Free tier. The
  alternatives are either paid managed Postgres on another provider (recurring bill, defeating the same cost rule
  that drove the cloud choice in the first place) or self-hosted Postgres on the Oracle ARM node, which works but
  eats into the 24 GB of RAM that the rest of the workloads are sharing and adds the operational burden of being
  our own DBA (backups, point-in-time recovery, version upgrades, tuning).
- Oracle's own Autonomous Database (Oracle DB flavor) is available on Always Free. The blocker there is the Go
  side: the Oracle DB drivers for Go are awkward (`godror` requires the Oracle Instant Client to be bundled at
  runtime, pure-Go alternatives are limited, and the community is small). For a Go-first codebase
  (see [ADR 0002](/adr/0002-adoption-of-go-as-primary-service-language/)) that is a meaningful tax on every service
  that touches the database.
- Oracle's **MySQL HeatWave** service is also part of Always Free, with substantially more headroom than the other
  free options we looked at, and the Go driver story for MySQL is excellent (`go-sql-driver/mysql` is pure Go,
  mature, and ubiquitous).

What we actually want out of the HeatWave service is not HeatWave's accelerator features (the in-memory analytics
engine, the Lakehouse integration, the ML extensions). We are not going to use those. What we want is the
underlying MySQL engine that HeatWave is built on, hosted by Oracle, sized at **8 GB of RAM and 50 GB of storage**
at no cost. That allocation is significantly larger than what other free-tier managed databases offer, it sits in
the same Montreal region as our ARM node, and it is fully managed (backups, patching, HA at the storage layer) so
we are not spending our own time on it.

Laying down our requirements:

- Relational, with proper SQL, transactions, foreign keys, and indexes.
- Free at the scale we need, on a tier that does not expire after 12 months.
- A mature, pure-Go driver, so services do not have to bundle a vendor client library.
- Per-service schema isolation, so each service can own its own tables without sharing a namespace with another
  service.
- A SQL dialect close enough to Postgres that a future move (if budget allows a managed Postgres later) is a
  reasonable amount of work, not a rewrite.

## Decision

Based on the requirements above, **MySQL HeatWave on OCI** is the relational database of choice. The accelerator
features are ignored; we use it as a managed MySQL 8 instance.

- **Capacity at no cost.** The Always Free HeatWave shape gives us 8 GB of RAM and 50 GB of storage in Montreal,
  which is the most generous free relational allocation we found and is large enough to carry the system for the
  foreseeable future without touching the budget.
- **Go driver quality.** `github.com/go-sql-driver/mysql` is pure Go, well maintained, and works through the
  standard `database/sql` interface. No vendor runtime, no CGO, no Oracle Instant Client to bundle.
- **Per-service schema isolation.** Each service owns its own MySQL schema (database in MySQL parlance).
  Cross-service reads happen through APIs and events, not through cross-schema joins, which keeps the service
  boundaries we set in [ADR 0001](/adr/0001-rewriting-to-microservices/) honest at the data layer too.
- **Standards proximity to Postgres.** MySQL 8 is closer to Postgres than older versions were (window functions,
  CTEs, JSON functions, CHECK constraints, sane utf8mb4 defaults). Combined with `database/sql` and a query style
  that does not lean on dialect-specific syntax, this keeps the door open to migrating to managed Postgres later if
  the budget allows it, without rewriting the data layer end to end.

We deliberately do not adopt Oracle DB despite it also being available on Always Free, because the Go driver
ecosystem around it is the limiting factor for our codebase. The technical merits of Oracle DB are not in question;
the cost of integrating it cleanly from Go is.

We also do not self-host Postgres on the ARM node, because the operational burden (backups, upgrades, replication,
PITR) on a one-person team is a more expensive bill than the cloud one we are avoiding.

## Consequences

- We give up Postgres-only features that we would have liked to use eventually (pgvector for any embedding work,
  PostGIS, `jsonb` operators, partial indexes, transactional DDL). Where those features become necessary, we either
  do the work outside the database or revisit this decision.
- MySQL has a few defaults that Postgres developers find surprising (case-insensitive identifiers depending on
  platform, looser type coercion in some edge cases, `REPEATABLE READ` as the default isolation level instead of
  `READ COMMITTED`). We pin the configuration we expect (`utf8mb4`, `READ COMMITTED`, strict SQL mode) at the
  schema level rather than relying on defaults.
- The database is a managed dependency on Oracle, and the blast radius of losing it is higher than for a compute
  node because data lives there. The PAYG posture (see
  [ADR 0004](/adr/0004-adoption-of-oracle-cloud/)) removes
  idle-reclaim and suspension risk, but standard database disaster-recovery practice still applies: regular logical
  backups (`mysqldump` or equivalent) stored off the provider, so that a worst-case account loss or operator error
  is recoverable.
- A future migration to managed Postgres is realistic but not free. By writing SQL that stays inside the common
  subset (no MySQL-specific extensions, UTC everywhere, no reliance on MySQL's `ON UPDATE CURRENT_TIMESTAMP`
  shortcuts, no zero-dates) we keep that cost bounded. Tooling like `sqlc` or `sqlx` over `database/sql` makes the
  driver swap the smallest part of the migration; the schema decisions are the larger one.
- Per-service schemas mean cross-service joins are off the table by design. Services that need data from another
  service ask for it through the documented API or event surface, which is consistent with the microservices
  posture and forces ownership boundaries to be respected at the data layer.
- HeatWave's accelerator features are present but unused. We accept that we are carrying (in cognitive overhead,
  not in money) capabilities we do not exercise. If a workload ever lands that genuinely benefits from in-memory
  analytics, we already have the engine sitting there.
- The fleet now has one more managed dependency to monitor. HeatWave's own availability becomes part of the system
  SLO whenever a service is on the request path of a query against it.

## Alternatives considered

- **PostgreSQL (self-hosted on the ARM node).** Our preferred engine on technical merit. Rejected because it would
  consume RAM that the rest of the workloads share, and because being our own DBA (backups, point-in-time recovery,
  upgrades, tuning) is more cost than offloading the database to a managed service that is free.
- **Managed PostgreSQL on another provider** (DigitalOcean Managed Postgres, Neon, Supabase, etc.). Technically the
  cleanest answer, but every option that gives us a useful size is a recurring monthly bill. That contradicts the
  cost rule that drove [ADR 0004](/adr/0004-adoption-of-oracle-cloud/),
  and free tiers on those providers are either too small or time-limited.
- **Oracle Autonomous Database (Oracle DB flavor) on Always Free.** Technically capable and free at a useful size,
  but the Go driver story is the blocker. `godror` requires the Oracle Instant Client bundled into every image
  that talks to the database, and the pure-Go alternatives are not at the maturity level we trust for production.
  The tax compounds across every service.
- **SQLite (per service, embedded).** Charming, zero-operational, and a tempting fit for the cost story, but a
  real multi-tenant system with concurrent writers across services is not where SQLite is at its best. We may
  still use it for local development or for narrow embedded use cases, but not as the primary store.
- **MariaDB on the ARM node.** Avoids vendor managed-service risk and keeps the same MySQL-shaped Go driver story,
  but reintroduces all of the self-hosting cost (backups, upgrades, tuning) that pushed us away from self-hosted
  Postgres in the first place. The managed HeatWave allocation is the better trade.
