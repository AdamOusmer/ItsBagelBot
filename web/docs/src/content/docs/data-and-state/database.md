---
# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
title: Database design
description: "Conceptual model, physical schemas per service, integrity rules, and the normalization argument for the data plane."
sidebar:
  order: 2
---

The design follows the classic two-step: a conceptual entity-relationship model first, then its mapping to physical
relational schemas. The twist is that the mapping is split across isolated MySQL schemas, one per data service
([ADR 0005](/adr/0005-adoption-of-mysql-heatwave/)), so relationships that cross a service boundary exist only in
the conceptual model and are enforced by the application, never by a foreign key.

## Conceptual model

Crow's foot notation. Solid lines are identifying relationships enforced by a foreign key inside one schema; dashed
lines are non-identifying, logical references across schemas, carried by a plain Twitch user ID column.

```mermaid
erDiagram
    USER ||--o{ TOKEN : "owns, cascade delete"
    USER ||..o{ COMMAND : "logical reference"
    USER ||..o{ MODULE : "logical reference"
    USER ||..o{ TEBEX_AGREEMENT : "logical reference"

    USER ||--o{ PREMIUM_GRANT : "owns, cascade delete"

    USER {
        uint64 id PK "Twitch user ID, natural key"
        string username
        string display_name
        string email UK "sensitive"
        bool is_active
        bool banned
        enum status "free, paid, vip"
        string locale
        bool commands_page_hidden
        int64 state_revision
        datetime created_at
        datetime updated_at
    }
    PREMIUM_GRANT {
        int id PK
        uint64 user_id FK
        string giveaway_id
        string award_id
        enum state "prepared, committed, cancelled, expired"
    }
    TOKEN {
        int id PK
        enum type "access_token, user_token"
        bytes token "Tink AEAD ciphertext"
        bytes refresh_token "nullable, ciphertext"
        enum platform "twitch"
        datetime access_token_expires_at "nullable"
        uint64 user_tokens FK "owning user"
    }
    COMMAND {
        int id PK
        uint64 user_id "indexed, no FK"
        string name
        json aliases "nullable"
        string response
        bool is_active
        bool stream_online_only
        string perm
        uint cooldown
        uint user_cooldown
        uint64 allowed_user_id
        int64 uses
        string bump_counter
        datetime created_at
        datetime updated_at
    }
    MODULE {
        int id PK
        uint64 user_id "indexed, no FK"
        string name
        bool is_enabled
        json configs "nullable"
        int revision
        datetime updated_at
    }
    TEBEX_AGREEMENT {
        string id PK "Tebex agreement ID"
        uint64 user_id "indexed, no FK"
        string recurring_reference UK
        string provider_status
        datetime paid_through_at
    }
```

## Physical schemas

Each service generates its schema from its own ent definitions and migrates it on startup. The session settings are
pinned at the connection level rather than trusted as server defaults: `utf8mb4`, `READ COMMITTED`, strict SQL mode,
UTC.

### `bagel_users` (users service)

The only schema with real foreign keys, because users, tokens and Premium grants live in the same bounded context
and change together (a login refreshes both user and token inside one transaction). Both edges cascade on delete.
The diagram shows the columns that matter for the model; `app/db/users/ent/schema/` has the full set, including
subscription, billing-event, onboarding and test-account fields.

```mermaid
erDiagram
    USERS ||--o{ TOKENS : "FK, on delete cascade"
    USERS {
        uint64 id PK
        string username
        string email UK
        bool is_active
        bool banned
        enum status
        string locale
        datetime created_at
        datetime updated_at
    }
    TOKENS {
        int id PK
        enum type
        bytes token
        bytes refresh_token
        enum platform
        datetime access_token_expires_at
        uint64 user_tokens FK
    }
```

Indexes: unique on `email`, composite on `(status, subscription_source, subscription_expires_at)`, a deliberately non-unique index on `username` (a rename frees the old login while the row still
carries it), and a unique composite on `(type, platform, user)` so a user holds at most one token per type and
platform. The token columns store only Tink AEAD ciphertext; the
associated data binds each envelope to its owner, type, and platform, so a ciphertext copied onto another row fails
authentication on decrypt.

### `bagel_commands` (commands service)

```mermaid
erDiagram
    COMMANDS {
        int id PK
        uint64 user_id "indexed"
        string name
        json aliases
        string response
        bool is_active
        bool stream_online_only
        string perm
        uint cooldown
        uint user_cooldown
        uint64 allowed_user_id
        int64 uses
        string bump_counter
        datetime created_at
        datetime updated_at
    }
```

The service also owns the custom-fetch definitions and their key labels (`fetchdefinition`, `fetchkey`) and the
command-use batch receipts (`commandusebatch`). Indexes: unique composite on `(user_id, name)`, which both enforces "one command name per channel" and serves the
per-channel lookup.

### `bagel_modules` (modules service)

```mermaid
erDiagram
    MODULES {
        int id PK
        uint64 user_id "indexed"
        string name
        bool is_enabled
        json configs
        int revision
        datetime updated_at
    }
```

The service also owns quotes, integration credentials and feed counters (`app/db/modules/ent/schema/`).
Indexes: unique composite on `(user_id, name)`. The `configs` column is opaque JSON owned by the module that reads
it; the database only guarantees it is valid JSON of bounded size (see the integrity rules below).

### `bagel_transactions` (transactions service)

```mermaid
erDiagram
    TEBEX_AGREEMENTS {
        string id PK
        uint64 user_id "indexed"
        string recurring_reference UK
        string provider_status
        datetime paid_through_at
    }
    TEBEX_WEBHOOK_EVENTS {
        string id PK
        string event_type
        enum status
        string transaction_id
        uint64 user_id
    }
```

Billing is idempotent by construction: webhook events and agreements use the provider's ID as the natural key, so
a retried webhook finds the existing row. The same schema holds the giveaway ledger (giveaways, draws, candidates,
awards, fulfillment plans, outbox, alerts, user leases), billing operations and award emails. See
[ADR 0011](/adr/0011-transactions-owns-giveaway-winner-records/).

### Other schemas

| Schema | Service | Holds |
|---|---|---|
| `bagel_loyalty` | loyalty | Viewer balances, counters, counter entries and batch receipts |
| `bagel_notifications` | notifications | Notification records and read state |
| `bagel_discord` | discord-data | Guild bindings and configuration, member XP, tickets and transcripts |

## Integrity rules

Integrity is enforced in two layers. The schema layer carries what the database can express: primary keys, unique
constraints, enum domains, NOT NULL, and the in-schema foreign keys with cascade. The application layer
(`internal/domain/validate`) carries the domain constraints the database cannot, applied at every repository
boundary, rejecting rather than rewriting:

| Input | Rule |
|-------|------|
| User ID | Non-zero |
| Username | 1-25 characters of `[a-zA-Z0-9_]` |
| Email | RFC 5322 address, 254 max, no smuggled display name or CRLF |
| Command name | 1-64 printable ASCII characters, no whitespace |
| Command aliases | At most 25, each a valid command name, unique |
| Command response | 1-5 lines, each 1-500 characters, no control characters |
| Command permission | One of `everyone`, `sub`, `vip`, `mod`, `lead_mod`, `broadcaster` |
| Command cooldown | 0 to 86400 seconds |
| Module name | 1-64 characters of `[a-z0-9_-]`, strict because it becomes part of a Valkey hash field |
| Module config | Valid JSON, 16 KiB cap |
| Token | 1 byte to 8 KiB, stored only as ciphertext |

ent parameterizes every query, so SQL injection is closed at the access layer; the rules above target domain
validity, resource caps, and projection key safety.

## Normalization

Every table is in BCNF. The argument is short because the schemas are deliberately narrow:

- `users`: all non-key attributes depend on the Twitch ID alone; `email` is an additional candidate key and
  determines nothing beyond itself.
- `tokens`: the candidate key `(user, type, platform)` determines the ciphertext columns; the surrogate `id` exists
  for ent's benefit, not as a hiding place for dependencies.
- `commands` and `modules`: all attributes depend on the candidate key `(user_id, name)` in full; there are no
  partial or transitive dependencies.
- `tebex_agreements` and `tebex_webhook_events`: every attribute depends on the provider's ID alone, nothing to decompose.

The one deliberate denormalization in the system lives outside MySQL: the Valkey projection duplicates status and
module state as a read model (see [Settings projection](/data-and-state/projection/)). That copy is a cache,
rebuildable at any time, and never the system of record.
