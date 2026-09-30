# Projector context

Projector is the Go read-model service: it folds data-owner events into shared Valkey projections and serves cache-oriented reads. It owns projection writes, hydration orchestration, live counter views and leaderboard seeding, while the DB services retain ownership of persistent business records.

See the [shared vocabulary](../../CONTEXT.md) and [context map](../../CONTEXT-MAP.md). Persistent Loyalty counters and Projector's live counter projections are separate views of activity; do not treat a projection as the durable ledger.

## Language

| Term | Meaning here |
| --- | --- |
| Projection | A read-oriented representation of owner data, such as a channel's status, commands, or modules. |
| Fold | Applying a validated owner event to the projection, followed by cache invalidation. |
| Hydration | Loading full user/modules/commands sections from owner RPCs to repair or warm the shared read model. |
| Seed | A foreground-loaded commands/modules section reused by background hydration; a known empty section is still a valid seed. |
| Tier | A read layer: process cache, shared Valkey, or owner RPC fallback. The health endpoint's “data tier” instead names a group of DB services. |
| Invalidation scope | The section to evict, such as status, modules, commands, or live; it is not a reply-template resolver scope. |
| Account incarnation | An account's creation identity used to stop old changes/deletions from reviving or removing a newer account. |
| Counter baseline | A snapshot of lifetime loyalty counters at go-live, used to calculate current-stream overview values. |
| Live counter | Fast bot/channel total projected from counter-bump events and seeded from Loyalty. |
| Board | Shared ranked channel view for processed message/event totals, initialized from Loyalty. |

## Code navigation

| Location | What it does |
| --- | --- |
| [main.go](main.go) | Loads subjects, provisions input streams, registers event folds/RPCs, and aggregates data-tier health. |
| [projector.go](projector.go) | User/module/command change folds, deletion protection, stream lifecycle, go-live warming and counter baseline. |
| [loyalty.go](loyalty.go) | RPC adapter for Loyalty totals and boards. |
| [counters.go](counters.go) | Counter-bump folding, seed timestamps, live totals, board initialization/retry and account cleanup. |
| [hydration/hydrator.go](hydration/hydrator.go) | Background section filling/refreshing with bounded concurrency, per-user gates, and fetch retries. |
| [rpc/dashboard.go](rpc/dashboard.go) | Commands/modules projection reads and replacements; cold reads call owners and seed hydration. |
| [rpc/status.go](rpc/status.go) | Broadcaster account-status read with cache and Users fallback. |
| [rpc/live.go](rpc/live.go) | Live reads and cold-read escalation to Twitch Outgress's system lane. |
| [rpc/streaminfo.go](rpc/streaminfo.go) | Stream metadata projection reads. |
| [shared projection storage/client](../../internal/projection/) | Actual key layout, serialization, revision checks, TTLs, read-client caches, and fetch definitions. |
| [projection contracts](../../internal/domain/rpc/projection/projection.go), [Projector contracts](../../internal/domain/rpc/projector/) | Owner snapshot and Projector-facing RPC shapes. |

## Flow and service boundaries

1. Users, Commands, and Modules publish `data.*` changes. One Projector fleet durable group consumes those, Loyalty counter bumps and Twitch stream-status events.
2. `foldEvent` decodes and validates before mutation. Invalid payloads are logged and acknowledged; storage failures return errors for redelivery.
3. Store writes precede invalidation fan-out. Account incarnation/revision protections live in the shared projection store; don't replace them with unconditional hash overwrites.
4. A cold dashboard read calls the owning service over NATS and returns that section; `EnsureAsync` repairs missing settings sections off the foreground path.
5. A go-live event synchronously writes live state and invalidates readers, then triggers full `RefreshAsync`, token warming, and best-effort counter-baseline capture. The current Projector handler calls `SetStreamLive` without the ingress receipt version; do not assume every lifecycle writer has the same ordering protection.
6. `HandleCounterBumps` updates bounded live counter views; missing views seed from Loyalty. `SeedBoards` initializes processed-message/event rankings at startup and retries unavailable reads.

## Editing rules and pitfalls

- A projected empty commands/modules list differs from an absent section. Preserve marker/seed semantics so empty configurations do not trigger repeated owner reads.
- `EnsureAsync` fills missing sections; `RefreshAsync` forces refresh. Waiting for hydration gates belongs in background work, not an RPC handler.
- Hydration fetch retries are bounded; store failures do not replay stale snapshots blindly. Protect newer events from older hydration responses.
- Emit invalidations after successful shared-store mutation. Core NATS broadcasts evict every process cache; a queue subscription would notify only one replica.
- Stream lifecycle writes must remain synchronous before acknowledging the source event. Go-live side effects are best effort and must not turn into lifecycle redelivery requirements.
- This service aggregates Users, Commands, Modules, Loyalty, Notifications, and Discord-data health. Transactions has its separate billing endpoint.
- Live counter seeding stamps after owner reads so an already-included batch is not counted again. Keep dedup, source-storage-time and seed-barrier behavior in the shared projection implementation.
- Incoming AutoMod currently lives in [Sesame](../twitch/sesame/context.md). This service has no AutoMod journal RPC or Rust enforcement role in the merged source.

## Focused validation

Run from the repository root:

```sh
go test ./app/projector/... ./internal/projection/...
go test ./app/projector/hydration ./app/projector/rpc
go test ./app/projector -run 'Counter|Stream|Watchtime'
```

For new projected fields, follow the whole path: owning schema/repository → event/owner snapshot contract → shared store → Projector fold → consuming client.
