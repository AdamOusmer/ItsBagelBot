# Loyalty data service

Agent guide for `app/db/loyalty`. See the [shared vocabulary](../../../CONTEXT.md)
and [app map](../../../CONTEXT-MAP.md).

## Responsibility and boundaries

Owns channel/viewer point standings, lifetime watch seconds, named counter
definitions and buckets in the MySQL schema selected by `DB_SCHEMA`. It persists decisions made by
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
| Receipt | SQL record of an admitted counter batch and its payload digest, committed with its effect; Valkey caches completion. |
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
| [repository/balance_reset.go](repository/balance_reset.go) | `balance.reset`: one bulk UPDATE zeroing a channel's points, watch seconds kept. |
| [repository/trial.go](repository/trial.go) | Once-only promotion of trial statistics and lock ordering. |
| [repository/counter_processor.go](repository/counter_processor.go), [counter_receipts.go](repository/counter_receipts.go) | Bounded active batching, Valkey claims/receipts and retry classification. |
| [repository/batches.go](repository/batches.go) | Trial-promotion and bump validation helpers shared by the counter persist path. |
| [repository/counter_persist.go](repository/counter_persist.go) | SQL transaction, deterministic lock order, bounded writes and row-range isolation. |
| [repository/counter_inbox.go](repository/counter_inbox.go) | Atomic SQL receipt admission, binary identities and payload mismatch rejection. |
| [repository/counter_cached.go](repository/counter_cached.go) | Batched SQL digest checks for completed Valkey receipts, including legacy markers. |
| [repository/counter_retention.go](repository/counter_retention.go) | Bounded receipt pruning on the repository's periodic flush loop. |
| [repository/counter_fences.go](repository/counter_fences.go), [counter_rows.go](repository/counter_rows.go) | Bulk owner fences and counter statements bounded by rows, bytes and placeholders. |
| [repository/watchtime.go](repository/watchtime.go), [watchtime_commit.go](repository/watchtime_commit.go) | Raw SQL inbox/dedup/fence schemas, per-chunk award commits and account deletion/recreation. |
| [repository/watchtime_retention.go](repository/watchtime_retention.go) | Monotonic retirement barrier and bounded replay-history pruning. |
| [rpc/rpc.go](rpc/rpc.go) | All `balance.*`, `top.get`, `counter.*` handlers and wire views. |
| [ent/schema/](ent/schema/) | Balance, Counter and CounterEntry schemas; watchtime auxiliary tables use raw SQL. |
| [repository/schema.go](repository/schema.go) | `MigrateSchema`: on MySQL, balances, counters and counter entries are clustered on their natural key. |

Shared contracts: [loyalty RPC](../../../internal/domain/rpc/loyalty/),
[loyalty events](../../../internal/domain/event/data/loyalty_events.go),
and [internal/watchtime](../../../internal/watchtime/).
Generated Ent code is not the schema source.

## Flow and contracts

- Default RPC prefix is `rpc.loyalty`; balance verbs require a real owner,
  counter verbs can admit the reserved bot namespace `user_id="0"`.
- Grouped earned events accumulate in memory and bulk-add standings. Reads hit
  Ent directly; a just-accepted legacy delta may not yet appear in an RPC read.
- Grouped counter events wait for `CounterProcessor.Process` before acknowledgment.
  Missing DTO BatchID falls back to transport UUID during rolling upgrades.
  Owner 0 commits in its own bot transaction alongside channel work. Each
  delivery's receipt and effect stay in the same transaction.
- Counter receipts and their counter effects commit in one SQL transaction, then
  complete a 70-minute Valkey receipt (`CounterReceiptTTL` in shared events).
  SQL→Valkey crash gaps and cache outages cannot recount a retained SQL receipt.
  The SQL batch identity is the SHA-256 hex digest of the original BatchID, so
  accepted IDs up to 256 bytes fit `VARBINARY(64)` without truncation. Payload
  hashes use `BINARY(32)`, admission owners `BINARY(16)`, and `created_at` indexed
  Unix seconds. SQLite uses BLOB columns and binary bindings.
- Counter and earned receipt inserts use affected rows to admit fresh deliveries.
  Duplicate receipts trigger a payload hash read. `pkg/db` pins MySQL
  `ClientFoundRows` off so duplicate no-op updates report zero affected rows.
- Loyalty enables MySQL multi-statements on its service pool. A normal counter
  batch sends BEGIN, the fence read (for channels), and receipt admission in
  one round trip, then writes counter effects and commits in a second. SQLite
  runs the same statements sequentially. Missing fences restart admission in
  fence-before-receipt order. The server runs the rest of a sent packet even
  after the client times out, so a batch reported as failed may still have
  committed; its receipt makes the retry a no-op.
- Sesame retries unconfirmed counter batches indefinitely with the same BatchID
  and immutable payload. Pending batches ignore early flush nudges and retry on
  the five-second ticker with exponential backoff capped at 30 seconds.
  Each queue suppresses only its own early flush nudges. Shutdown attempts all
  pending and newly aggregated batches regardless of backoff within five seconds.
  Pending batches stay in memory and new increments aggregate behind them;
  process termination can still lose work unconfirmed after the final flush.
- SQL receipts live for 24 hours (`CounterReceiptRetention` in shared events),
  measured from the original SQL admission. The 15-second repository loop
  deletes expired rows in ordered chunks of 1,000, at most 64 chunks or two
  seconds per pass. At 200 unique deliveries/s this retains 17,280,000 rows/day
  at roughly 200-250 bytes/row including indexes, about 3.5-4.3 GB for 24 hours,
  and adds 3,000 rows per loop; the row limit permits pruning 64,000 rows per
  loop (about 4,267 rows/s), subject to the time budget and database throughput.
  Later passes drain any backlog. A batch republished or replayed from DLQ after
  24 hours is not deduplicated once its SQL receipt has been pruned; indefinite
  producer retries do not extend this deduplication horizon. No DLQ replay tool
  exists in the tree.
- Valkey retains the original receipt key and `done` marker during rolling
  upgrades. Completed markers with a SQL receipt get a batched digest check;
  legacy markers without a SQL receipt remain authoritative until they expire.
  SQL detects payload mismatches as `ErrCounterReceiptMismatch`, which wraps
  `ErrInvalidInput`; the processor logs `user_id` and the original `batch_id`.
- Invalid-input failures split merged transactions to isolate poison deliveries.
  Transient errors return to every delivery without splitting. MySQL 1213/1205
  retries restart the whole transaction through `pkg/db.RetryTx`.
- Watch awards use a separate Valkey outbox consumer. Each viewer chunk commits
  its receipts and balance credits together. The first chunk creates an incomplete
  operation row with the payload hash; every chunk checks it under the account
  fence, and the last chunk marks it complete. Viewer receipts make retries of
  partially committed awards idempotent; errors leave delivery pending.
- Loyalty watch-award consumers and watch-history pruning must not overlap
  versions that write incomplete operations with older versions that treat every
  operation row as complete. Stop and drain every old loyalty replica, including
  in-flight watch-award handlers and pruning, before starting any new replica;
  keep unpaid outbox deliveries pending for the new consumers. A rolling update
  with old and new loyalty replicas active together is unsafe. The older code
  can acknowledge a matching incomplete operation without paying its remaining
  viewers and can prune its operation and viewer receipts. Hiding progress in a
  separate table cannot fix the older closed-window path, which acknowledges
  without checking for unpaid chunks. Before rollback, finish every incomplete
  operation with the new version, then stop and drain every new replica before
  starting any old replica.
- User change/delete events restore or retire account instances using source
  timestamps. Watchtime health and the grouped event lane are readiness checks.

## Invariants and pitfalls

- Keep the three durability paths distinct: legacy earned accumulation is
  loss-tolerant, counters acknowledge committed SQL with durable bounded dedup,
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
  these to JavaScript Number or the former 2^53 ceiling. Guarded upserts leave
  saturated counter values unchanged without logging a warning and apply the
  other rows in the batch. Transient/uncertain commits retry.
- Migrate through `MigrateSchema`, never `client.Schema.Create` alone: on MySQL the
  balance and counter tables' primary key is the natural key and Ent's `id` is only
  a unique auto-increment column. An auto-increment primary key makes replicas
  writing different channels deadlock or wait. MySQL fence reads rely on READ COMMITTED, where
  an exact-key miss takes no gap lock. Insert the missing fence and read it again
  under lock before applying effects.
- Preserve the global lock order: retention barrier, account fence, receipts and
  operations, `counters`, `counter_entries`, then `balances`. Use ascending owner
  and natural key order for multi-row writes. Lifecycle cleanup holds the exclusive
  fence across its deletes.
- Watch chunks take a shared retention barrier lock. Pruning advances the barrier
  with one guarded autocommit update. Counter and earned transactions take shared
  account fences. Watch, lifecycle and trial promotion transactions take exclusive
  account fences; promotion skips a retired account. A batch's negative entry
  bumps apply only to buckets its ascending lock read found, so a bucket created
  after that read is never updated out of lock order.
- Prune watch replay markers only after the same source-window barrier rejects
  replay and is capped below retained unpaid outbox work. Counter receipts use
  the separate bounded delivery horizon above. Plain age-based deletion of watch
  markers is unsafe. Incomplete operation rows survive pruning; a finalized partial
  award returns an error on redelivery rather than acknowledging unpaid viewers.
  Account admission refusing a retired or replaced instance completes an existing
  operation in a committed transaction so normal retention can prune its history.
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
MySQL lock tests run only with `MYSQL_TEST_DSN`; Valkey integration
tests skip without `valkey-server`. See each test before selecting infrastructure.
CI regeneration and race checks: [main.yml](../../../.github/workflows/main.yml).
