# Loyalty data service

Agent guide for `app/db/loyalty`. See the [shared vocabulary](../../../CONTEXT.md)
and [app map](../../../CONTEXT-MAP.md).

## Responsibility and boundaries

Owns channel/viewer point standings, lifetime watch seconds, named counter
definitions and buckets in `bagel_loyalty`. It persists decisions made by
Sesame/watchtime producers; it does not decide whether a viewer was watching.
Loyalty module enablement/configuration belongs to Modules, account identity to
Users. The service consumes deltas and watch awards and exposes direct SQL-backed
RPC reads and synchronous balance/counter management.

## Nomenclature

| Term / identifier | Meaning here |
| --- | --- |
| `user_id` | Channel owner (Twitch broadcaster), except reserved counter namespace 0. |
| `viewer_id` | Twitch chatter; registration as a bot account is not required. |
| Balance / standing | Spendable `points` plus lifetime `watch_seconds` for one channel/viewer. |
| Earned delta | Legacy loss-tolerant summed additions received via `data.loyalty.earned`. |
| Watch award | Durable window posting delivered from the Valkey watchtime outbox. |
| Account instance | Users' creation timestamp (`AccountCreatedAt`), separating recreation from stale work. |
| Fence | SQL lifecycle state preventing retired-account events from restoring rows. |
| Counter | Named definition and scope; total value resides here for pooled scopes. |
| Counter entry | Bucket value addressed by owner/name/command/viewer. |
| Scope | `bot`, `channel`, `viewer`, `command`, or `viewer_command`; shared constants are authoritative. |
| `BatchID` | Stable counter-delivery identity, reused on retry. |
| Receipt | Valkey completion cache for the active processor; separate CounterBatch SQL receipts serve ApplyBumps. |
| Trial promotion | One-time carry of observed decoded/answered totals into registered channel counters. |
| Wager | Atomic settlement adding/removing a stake according to a caller-supplied outcome. |

## Where to start

| File | Role |
| --- | --- |
| [main.go](main.go) | Earned/lifecycle/counter consumers, 32 counter handlers, Valkey watchtime consumer and drain order. |
| [repository/loyalty.go](repository/loyalty.go) | Legacy earned accumulation, 15-second flush, additive SQL upserts and shutdown coordination. |
| [repository/queries.go](repository/queries.go) | Balance read/set/add/spend/top and counter management/read APIs. |
| [repository/transfer.go](repository/transfer.go) | Transactional guarded debit and recipient credit/create. |
| [repository/balance_adjust.go](repository/balance_adjust.go), [wager.go](repository/wager.go) | Resolved-viewer balance adjustment and atomic wager settlement. |
| [repository/trial.go](repository/trial.go) | Once-only promotion of trial statistics and lock ordering. |
| [repository/counter_processor.go](repository/counter_processor.go), [counter_receipts.go](repository/counter_receipts.go) | Bounded active batching, Valkey claims/receipts and retry classification. |
| [repository/batches.go](repository/batches.go), [prune.go](repository/prune.go) | Separate ApplyBumps SQL receipt writer and retention helpers; not the grouped consumer path wired in main. |
| [repository/counter_persist.go](repository/counter_persist.go) | SQL transaction, deterministic lock order, bounded writes and row-range isolation. |
| [repository/watchtime.go](repository/watchtime.go) | Raw SQL inbox/dedup/fence schemas, atomic awards and account deletion/recreation. |
| [repository/watchtime_retention.go](repository/watchtime_retention.go) | Monotonic retirement barrier and bounded replay-history pruning. |
| [rpc/rpc.go](rpc/rpc.go) | All `balance.*`, `top.get`, `counter.*` handlers and wire views. |
| [ent/schema/](ent/schema/) | Balance, Counter, CounterEntry and CounterBatch schemas; watchtime auxiliary tables use raw SQL. |

Shared contracts: [loyalty RPC](../../../internal/domain/rpc/loyalty/),
[loyalty events](../../../internal/domain/event/data/loyalty_events.go),
and [internal/watchtime](../../../internal/watchtime/).
Generated Ent code is not the schema source.

## Flow and contracts

- Default RPC prefix is `bagel.rpc.loyalty`; balance verbs require a real owner,
  counter verbs can admit the reserved bot namespace `user_id="0"`.
- Grouped earned events accumulate in memory and bulk-add standings. Reads hit
  Ent directly; a just-accepted legacy delta may not yet appear in an RPC read.
- Grouped counter events wait for `CounterProcessor.Process` before acknowledgment.
  Missing DTO BatchID falls back to transport UUID during rolling upgrades.
- Active counter writes commit in SQL, then complete a 70-minute Valkey receipt
  (`CounterRepublishWindow + 10 minutes` in shared events).
  SQL→Valkey crash gaps or later replays can count again; do not call this exactly once.
- Watch awards use a separate Valkey outbox consumer. Inbox, window/viewer dedup
  and balance credit commit together; errors leave delivery pending.
- User change/delete events restore or retire account instances using source
  timestamps. Watchtime health and the grouped event lane are readiness checks.

## Invariants and pitfalls

- Keep the three durability paths distinct: legacy earned accumulation is
  loss-tolerant, counters acknowledge committed SQL with best-effort short dedup,
  watch awards use durable SQL posting and lifecycle fences.
- Spend/transfer guards require `points >= amount` in the actual debit statement.
  Transfer credits only in the same transaction. A resolved TargetViewerID allows
  creating the recipient's first balance; legacy login-only calls need an existing
  row. BalanceAdjustViewer can likewise create a resolved target's first standing.
  Wagers settle net stake changes atomically and check insufficient funds/headroom.
- Normalize counter names and command buckets consistently. Existing counter
  scope wins on implicit creation; bucket shape must follow that scope.
- Counter values stay within nonnegative signed-int64 range. Counter wire values
  are decimal JSON strings; balance replies also carry points_exact. Do not narrow
  these to JavaScript Number or the former 2^53 ceiling. Saturated rows can be
  rejected separately; transient/uncertain commits retry.
- Lock retention barrier before account fences, then other posting rows. Counter
  persistence locks owner fences in ascending order; preserve shared lock order.
- Prune replay markers only after the same source-window barrier rejects replay
  and is capped below retained unpaid outbox work. Plain age-based deletion is unsafe.
- Retired-instance events are consumed without resurrecting state; source account
  creation time, not arrival time, determines whether recreation is newer.
- Counter trial promotion is once-only; writes racing promotion recheck and retry
  the rolled-back transaction with redirected totals. Keep system/trial names reserved.

## Focused checks

From repository root:

```sh
go test ./app/db/loyalty ./app/db/loyalty/repository ./app/db/loyalty/rpc
go run -mod=mod entgo.io/ent/cmd/ent generate --feature sql/upsert,sql/lock ./app/db/loyalty/ent/schema
```

Use watchtime/retention tests for durable awards, transfer tests for ledger moves,
and counter processor/range tests for batching; wager, balance-adjustment and
trial tests cover their own concurrency invariants. SQLite range coverage uses CGO.
Real MySQL watchtime tests opt in through `MYSQL_TEST_DSN`; Valkey integration
tests skip without `valkey-server`. See each test before selecting infrastructure.
CI regeneration and race checks: [main.yml](../../../.github/workflows/main.yml).
