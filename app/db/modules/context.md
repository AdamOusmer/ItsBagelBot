# Modules data service

Agent guide for `app/db/modules`. Read the [shared vocabulary](../../../CONTEXT.md)
and [app map](../../../CONTEXT-MAP.md).

## Responsibility and boundaries

Owns broadcaster module enablement/config JSON, channel quote books, lifetime
personality feed tallies, and sealed Govee/Spotify credentials in `bagel_modules`.
Sesame implements module behavior, Gossip contacts external integrations, and
Projector maintains the shared configuration projection. Per-guild Discord
settings belong to Discord Data; the Discord module retains channel-level controls.
Loyalty module configuration lives here, but balances/counters live in Loyalty.

## Nomenclature

| Term / identifier | Meaning here |
| --- | --- |
| `user_id` | Owning broadcaster's numeric Twitch ID. |
| Module / `ModuleView` | Named feature's enabled flag, configuration blob and revision. |
| `configs` | Module-specific JSON; shared validators and runtime module define its shape. |
| `Set` / `SetNow` | Batched background replacement / synchronous interactive save returning committed views. |
| `Patch` / `PatchExisting` | Partial-key merge / update guarded by both existing row ID and revision. |
| Revision / `__rev` | Concurrency version represented in durable row/config handling. |
| `__account_created_at` | Internal loyalty-config stamp for the Users account incarnation. |
| Quote number | Channel-local public quote identifier, distinct from Ent row ID. |
| Feed counter | Fixed global row 1 for lifetime bagel feed total. |
| Feed receipt | Permanent event identity and committed totals for retry-safe feeding. |
| Channel feed counter | Broadcaster-local lifetime feed total used for ranking. |
| Credential custody | Separate sealed storage; secrets never enter projected config blobs. |
| Spotify app / grant | Broadcaster-owned client credentials / granted refresh token and scopes. |

## Where to start

| File | Role |
| --- | --- |
| [main.go](main.go) | Boot, event consumers, Users account-instance resolver and RPC registration. |
| [repository/modules.go](repository/modules.go) | Cached lists, batched Set, immediate SetNow/Patch/PatchExisting, publication and reprojection. |
| [repository/loyalty_instance.go](repository/loyalty_instance.go) | Loyalty instance stamps and canonical-account-guarded snapshot cleanup. |
| [repository/quotes.go](repository/quotes.go) | Quote numbering, CRUD, random reads, date correction and race retries. |
| [repository/personality.go](repository/personality.go) | Atomic global/channel feed totals and leaderboard reads. |
| [repository/govee.go](repository/govee.go), [spotify.go](repository/spotify.go) | AEAD key/token/app custody and safe status metadata. |
| [rpc/dashboard.go](rpc/dashboard.go) | Module list/upsert/patch and integration-custody wiring. |
| [rpc/quotes.go](rpc/quotes.go), [personality.go](rpc/personality.go) | Quote and feed RPC surfaces. |
| [rpc/govee.go](rpc/govee.go), [spotify.go](rpc/spotify.go) | Dashboard credential setup plus internal unseal verbs. |
| [ent/schema/](ent/schema/) | Seven durable entities, including FeedReceipt; other Ent files are generated. |

Contracts: [modules RPC](../../../internal/domain/rpc/modules/),
[projection RPC](../../../internal/domain/rpc/projection/),
[data events](../../../internal/domain/event/data/data_events.go),
[validation](../../../internal/domain/validate/), and
[module reply-template fields](../../../internal/domain/modulevars/templates.go).

## Flow and contracts

- Default dashboard prefix `bagel.rpc.modules`: `list`, `upsert`, `patch`,
  `patch-existing`, with quote verbs under `.quote` and feed verbs under `.personality`.
- Dashboard upsert uses `SetNow`, persists immediately and returns committed views.
  Background `Set` still coalesces by `(user_id, name)` for 2 seconds / 256 items.
  Events publish persisted revisions; lists have a local five-minute cache.
- Patch reads current state, merges supplied keys and advances revision. Stale
  `ExpectedRev` produces `Conflict`; absent expected revision allows last-write-wins.
  `patch-existing` requires both ExpectedID and ExpectedRev, conflicts for missing
  or replaced rows, and never creates a new module.
- Module-change broadcasts invalidate every local cache; grouped reprojection
  requests republish persisted rows, and grouped account deletion sweeps owned data.
- `bagel.rpc.internal.projection.modules.get` serves secret-free repair views.
  Loyalty config resolves Users' current account creation timestamp before writes.
- Govee/Spotify use optional `TINK_KEYSET_PATH` custody, with distinct internal
  decrypt surfaces for Gossip. Read the RPC files for subject environment overrides.

## Invariants and pitfalls

- Keep interactive SetNow and Patch synchronous: replies carry committed revisions
  required by projection gates. Review revision/instance tests when changing writes.
  Generic ConfigsJSON validates JSON; module runtime code defines feature-specific fields.
- Loyalty configs retain their instance stamp. Missing canonical Users identity
  must be an explicit matching not-found reply, not an outage/refusal interpreted
  as absence. Deletion captures module/credential/quote rows, checks account
  incarnation, then transactionally deletes only unchanged captured rows.
  Newer stamped loyalty state prevents stale cleanup of a replacement account.
- Module configs are cleartext projections. Store Govee keys, Spotify refresh
  tokens and client secrets only through the custody repositories; AEAD labels
  distinguish secret types and owning broadcasters.
- Each broadcaster supplies their own Spotify app. Unknown stored scopes mean
  stale consent, not fully granted permissions. Rotation checks the previous token;
  a refresh failure marks matching stored consent for reconnect instead of silently
  clearing it. Rotation can preserve existing scopes.
- Quote numbers use max+1 and retain holes after deletion; do not renumber an
  existing quote book. Maximum text length is 450; conflicting adds retry boundedly.
- Global feed total and channel tallies are distinct durable values. The daily
  total and hot sorted-set view live in Sesame's Valkey, not these tables. Feed
  event receipts commit with global/channel increments and return stored totals on replay.
- Keep `ent/runtime` imported. `DeleteAccount` uses guarded snapshot cleanup; the
  older `DeleteAllForUser` helper has different best-effort custody semantics.

## Focused checks

From repository root:

```sh
go test ./app/db/modules ./app/db/modules/repository ./app/db/modules/rpc
go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/lock ./app/db/modules/ent/schema
```

Use `modules_test.go` and `patch_existing_test.go` for interactive saves/conflicts,
`loyalty_instance_test.go` for lifecycle guards, and credential/feed/quote tests
for the relevant sub-store.
CI regeneration/race checks: [main.yml](../../../.github/workflows/main.yml).
