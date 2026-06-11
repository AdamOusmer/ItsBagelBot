---
title: Class design
description: "UML class diagrams of the shared infrastructure and the repositories,
and the design patterns the data plane is built on."
sidebar:
  order: 4
---

The data plane separates domain contracts (`internal/domain`), reusable infrastructure (`pkg/`), and the service
code that composes them (`app/`). Dependencies point inward: repositories depend on interfaces and on the generic
infrastructure, never the other way around.

## Shared infrastructure

The crypto adapter realizes the domain's `Packer` interface, so the services depend on the abstraction and Tink
stays an implementation detail. The cache and the batcher are generic and carry no domain knowledge.

```mermaid
classDiagram
    direction LR

    class Packer {
        <<interface>>
        +Pack(plaintext, associatedData) SecureEnvelope
        +Unpack(envelope) plaintext
    }

    class SecureEnvelope {
        +Ciphertext bytes
        +AttachedData bytes
    }

    class Crypto {
        -primitive tink.AEAD
        +Pack(plaintext, associatedData) SecureEnvelope
        +Unpack(envelope) plaintext
    }

    Crypto ..|> Packer : realizes
    Packer ..> SecureEnvelope : creates

    class Cache~V~ {
        -shards sharded TTL maps
        -group singleflight.Group
        -ttl time.Duration
        -jitter time.Duration
        +GetOrLoad(ctx, key, loader) V
        +Set(key, value) void
        +Invalidate(key) void
        +Close() void
    }

    class Batcher~K,V~ {
        -pending map of K to V
        -interval time.Duration
        -maxSize int
        -flush Flush~V~
        +Add(key, value) void
        +Close(ctx) void
        -flushPending(ctx) void
    }
```

## Repositories

One diagram per shape: the users repository writes through directly and seals tokens; the modules repository (the
commands one is its twin) routes settings writes through the batcher and hands the batcher its `flush` method as
the callback. All collaborators arrive by constructor injection, which is what lets the tests substitute an
in-memory SQLite client and a recording publisher.

```mermaid
classDiagram
    direction TB

    class Publisher {
        <<interface>>
        +Publish(topic, messages) error
        +Close() error
    }

    class UserView {
        +ID uint64
        +Username string
        +IsActive bool
        +Status string
    }

    class Users {
        -client ent.Client
        -views Cache of UserView
        -packer Packer
        -pub Publisher
        +Register(ctx, id, username, email) error
        +Get(ctx, id) UserView
        +SetStatus(ctx, id, status) error
        +Delete(ctx, id) error
        +UpsertToken(ctx, userID, type, platform, access, refresh) error
        +Token(ctx, userID, type, platform) plaintext
        +Reproject(ctx) error
        +Invalidate(id) void
    }

    class ModuleView {
        +Name string
        +IsEnabled bool
        +Configs json
    }

    class Modules {
        -client ent.Client
        -views Cache of ModuleView slices
        -batcher Batcher
        -pub Publisher
        +List(ctx, userID) ModuleView slice
        +Set(userID, name, enabled, configs) error
        +Reproject(ctx) error
        +Invalidate(userID) void
        -flush(ctx, items) error
    }

    Users *-- "1" UserView : serves
    Users --> Publisher : announces
    Modules *-- "1" ModuleView : serves
    Modules --> Publisher : announces
    Modules ..> Modules : flush is the batcher callback
```

The composition (filled diamond) between a repository and its cache and batcher is deliberate: the repository
creates and owns them, and closing the repository closes them. The publisher and the packer are associations to
interfaces owned elsewhere, injected at construction.

## Patterns in play

| Pattern | Where | Why |
|---------|-------|-----|
| Repository (PoEAA) | `app/*/repository` | One object per aggregate mediating between domain and ent, the seam every test uses |
| Data Transfer Object | `internal/domain/event/data`, the view structs | Full-state payloads across the bus, sensitive-field-free views in the caches |
| Publish/Subscribe (Observer at system scale) | `pkg/bus` over NATS | Decouples writers from every reader: caches, projector, future consumers |
| Event-Carried State Transfer | All change events | Consumers update from the event alone; no service reads another's schema |
| Read-Through cache with request coalescing | `pkg/cache` | Singleflight guarantees one loader per key regardless of concurrency |
| Write-Behind | `pkg/batch` | Coalesces per key and lands one transaction per window instead of one write per click |
| CQRS, read model | `app/projector` and Valkey | The write side stays normalized in MySQL; the read side is a denormalized projection |
| Adapter | `pkg/crypto` (Tink behind `Packer`), `pkg/bus` (zap behind Watermill's logger) | Third-party APIs stay behind owned interfaces |
| Dependency Injection | Every `NewX` constructor | Composition happens in `main`, tests inject fakes |
| Idempotent Receiver | Every consumer, `Transactions.Record` | At-least-once delivery and webhook retries must not double-apply |

## Where the patterns are not

Just as deliberate as the patterns used are the ones avoided. There is no service locator and no global registry:
everything arrives by constructor. There is no shared kernel of entities between services: each owns its ent
schema, and the only shared types are the event DTOs. And there is no premature abstraction over the database: the
repositories speak ent directly, because swapping MySQL is already covered at the driver and dialect level
([ADR 0005](/adr/0005-adoption-of-mysql-heatwave/)).
