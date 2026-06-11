---
title: "0007 - Adoption of Per-Schema Data Microservices"
description: "Architecture decision record: Adoption of per-schema data microservices
as bounded contexts under app/, each owning its own MySQL schema"
---

**Date:** 2026-06-09

## Status

Accepted

## Context

The system needs a persistent data layer for four kinds of state: user records with their OAuth tokens and a paid
tier status (free, paid, or vip as a permanent paid tier), custom chat commands, module toggles with their
configurations, and Tebex transactions (only the transaction ID and the owning user; payment details stay on Tebex's
side).

[ADR 0005](/adr/0005-adoption-of-mysql-heatwave/) already settled that each service owns its own MySQL schema and
that cross-service reads happen through APIs and events, never through cross-schema joins. What it did not settle is
where the service boundaries sit. A first prototype kept every entity in one shared ent client under a single
`internal/db` package, which made the isolation rule easy to violate by accident: one client, one schema, and every
table a foreign key away from every other.

Three forces shape the split:

- Boundaries should follow change, not tables. A user record and its OAuth tokens always change together (a login
  refreshes both), so splitting them into separate services would turn every login into a distributed transaction.
  Commands and modules change independently of identity and of each other.
- The money path has different requirements than the settings path. Transactions and tier changes must be written
  immediately and audited; a module toggle can tolerate a short write-behind window
  (see [ADR 0008](/adr/0008-caching-and-write-behind-strategy/)).
- The team is one person. Every additional service is another binary to deploy, monitor, and reason about, so the
  count has to be justified by a real boundary, not by symmetry.

## Decision

We split the data layer into four bounded contexts, each a standalone Go service under `app/`, plus the projector
(see [ADR 0009](/adr/0009-adoption-of-valkey-for-the-settings-projection/)):

- **`app/users`**: user records (Twitch user ID as the primary key, username, email, active flag, tier status) and
  OAuth tokens. Tokens live inside this service because they are part of the identity lifecycle; within the schema
  they keep a real ent edge to the user with cascade delete. Tokens are stored only as Tink AEAD ciphertext, and the
  associated data binds each envelope to its owner, token type, and platform, so a ciphertext copied onto another
  row fails authentication on decrypt.
- **`app/commands`**: custom chat commands.
- **`app/modules`**: module on/off toggles and their JSON configurations.
- **`app/transactions`**: Tebex transaction records.

Each service owns its own MySQL schema (`bagel_users`, `bagel_commands`, `bagel_modules`, `bagel_transactions`),
generates its own ent client from its own `ent/schema` directory, and runs its own migrations on startup. There are
no foreign keys across services: commands, modules, and transactions reference the user by a plain indexed Twitch ID
column. Only the users service may resolve that ID into a user.

Shared plumbing that carries no domain knowledge lives in `pkg/` (database provider, cache, batcher, bus, crypto,
logger, monitoring), and the event contracts shared between services live in `internal/domain/event/data`. Each
repository validates its inputs at the boundary
(see the validation rules in [Data & State](/data-and-state/)).

Tests stay on the established pattern: every repository is tested against an in-memory SQLite database through
`enttest`, with a fake publisher capturing the events.

## Consequences

- Cross-context joins are impossible by construction, not by discipline. A feature that needs commands and user
  status together has to consume both through events or ask each service, which is the boundary working as intended.
- Deleting a user no longer cascades across the system through foreign keys. The users service deletes its own rows
  and publishes a deletion event; the other services and the projector converge from that event. A consumer that
  misses the event keeps orphaned rows until reconciliation, which is the standing trade-off of eventual
  consistency.
- Every service carries its own copy of the generated ent runtime, which costs binary size, not correctness.
- The user record and its tokens stay transactionally consistent inside one schema, so the login path needs no
  distributed coordination.
- Five binaries instead of one means five deployments and five things to monitor. The shared `pkg/` layer keeps the
  wiring uniform so the marginal cost of each service stays low.

## Alternatives considered

- **One service per table** (users, tokens, configs, commands, modules, transactions as six services). Maximum
  isolation on paper, but tokens without users is not a real boundary: every token operation would need a remote
  check against the users service, and the login path would become a distributed transaction. Rejected as symmetry
  for its own sake.
- **Two services** (identity holding users, tokens, and transactions; settings holding commands and modules).
  Tempting because tier changes are driven by payments, but it welds the money path and the identity path into one
  deployment, and Tebex webhook ingestion has a different availability and audit profile than logins. Rejected to
  keep the money path its own small, boring service.
- **Keep the shared `internal/db` ent client.** The least work today, but it makes the per-schema isolation from
  [ADR 0005](/adr/0005-adoption-of-mysql-heatwave/) a convention instead of a structure, and the first deadline
  would have produced the cross-table join we are trying to make impossible. Rejected.
