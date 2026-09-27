# Short-lived Valkey counter receipts

This change implements best-effort deduplication for `data.loyalty.counters` in the current source tree. Completed counter receipts expire after five minutes. Valkey operations and SQL writes are batched. It has not been deployed by this task, and no production storage has been reclaimed.

The September 27 storage audit observed production writing the SQL `bagel_loyalty.counter_batches` ledger with eight-day retention. This change was rebased onto current upstream source, preserving its stable counter IDs, confirmed publications, signed-delta limits and system counter guards. Reconcile running image digests and source versions before rollout. This document describes the new local implementation, not verified production behavior.

## Processing and limits

- Sesame's loyalty reporter aggregates counter deltas every five seconds and publishes chunks of up to 1,000 entries per broadcaster. Each immutable chunk gets a UUID `batch_id`. Bot statistics use the same reporter. Broker redelivery retains that payload and ID; an identical later window gets a new ID.
- The loyalty service uses 32 fixed counter handlers. Each waits for the processor result before acknowledging its message. The processor queue holds at most 256 messages and gathers a batch for up to 20 milliseconds, with limits of 128 messages and 8,192 bump entries. A single incoming message may contain at most 4,096 bumps. The 128-message cap is an upper bound; the fixed handler count usually limits a live batch to 32 requests.
- The processor explicitly pipelines owner-scoped claim scripts using Valkey `DoMulti`, then pipelines completion or release scripts with a second `DoMulti`. This batches network writes instead of issuing one synchronous Valkey call per message. Commands go to the primary connection. Each script touches one receipt key, so the pipeline does not require a cross-key transaction.
- Receipt keys are `loyalty:counter:receipt:<user_id>:<sha256(batch_id)>`. The broadcaster namespace prevents unrelated channels from sharing a receipt. Duplicate IDs within the collected batch share one SQL result.
- An atomic claim creates a 45-second processing lease. A completed receipt skips SQL. A receipt owned by another in-progress request returns a retryable error; it is never acknowledged as completed merely because someone else holds the lease.
- Claimed deliveries, deliveries without any identity, and deliveries whose Valkey claim failed because the cache is unavailable are persisted in bounded SQL transactions. A completed marker is written only after SQL commit, and its five-minute TTL starts then. SQL failures release only the current owner's lease; owner checks prevent late cleanup from deleting another request's newer lease. Transient SQL failures remain retryable.
- Valkey infrastructure failures are best effort: claim errors produce throttled warnings and allow SQL to proceed. A successful SQL commit is acknowledged even if receipt completion fails or the lease was lost, with a warning rather than a NACK. This prevents a cache outage from exhausting the counter retry budget or deliberately replaying an already committed window. An explicit in-progress owner collision still retries. Later redelivery can recount when the receipt could not be saved.
- Valkey phases have five-second timeouts; SQL processing and its persistence gate have a 30-second bound. Shutdown stops admissions and drains admitted work before closing the processor and Valkey connection.

Older payloads without `batch_id` use `msg.UUID` as their identity: the stable logical transport header, or the JetStream stream-sequence fallback when that header is absent. They retain Valkey deduplication across broker redelivery and still wait for SQL commit. Only a direct `Process` call without an identity, or a message lacking both payload and transport identity, bypasses deduplication. Do not synthesize an ID from payload contents: two legitimate windows can contain identical counter deltas.

## Accepted replay tradeoff

The current upstream data consumer allows three redeliveries, paced by 5-second, 20-second and 60-second NAK delays: 85 seconds of total backoff. Its handler deadline is 30 seconds and its server AckWait is four seconds, with progress signals while a handler runs. The producer retains an immutable failed or ambiguously acknowledged counter publication and retries it at subsequent five-second flushes, with a five-second bound per confirmed publish attempt. Its abandonment horizon is reduced from one hour to three minutes; once a flush sees that age, it logs and discards that pending publication before republishing. The same batch ID and payload remain unchanged across attempts. Three minutes plus the normal backoff leaves room within the five-minute completed receipt policy for bounded processing; backlog, outages and manual late replay can still outlive that policy. The command-use reporter's retry behavior is unchanged.

The SQL transaction and Valkey receipt are separate operations. A crash after SQL commit but before receipt completion can count a retry again. Lease loss, Valkey failover or eviction, infrastructure fail-open, replays after five minutes, and manual late replay can also recount. These are accepted for these counters. An error or uncertain SQL commit does not create a successful completed marker; a known successful SQL commit is acknowledged even if the completion write fails. Verify effective consumer settings and image versions at rollout rather than assuming deployed retry behavior matches this source.

Watchtime award receipts, watchtime history retention, balances, payments, billing, giveaways, and their replay policies are unchanged by this counter receipt change.

## Local validation

The affected bus, Valkey, loyalty service/repository, Sesame engine and event DTO packages passed their Go tests. Focused race checks passed for the concurrent consumer and counter handler/processor. The installed local Valkey server exercised the actual Lua claims, completed receipts, expiry, duplicate suppression and rollback/retry path; the batching tests also verify explicit `DoMulti` boundaries for both claims and completion writes.

The loyalty service built successfully for Linux amd64 with `CGO_ENABLED=0`. SQL transaction tests use disposable SQLite databases; no production MySQL write test or deployment was performed. Verify MySQL behavior and runtime settings in staging as part of rollout.

## Rollout and receipt-table retirement

1. Build and test the reconciled source, including the producer retry identity and three-minute expiry regressions, record image digests, and deploy new loyalty consumers first. Keep the legacy SQL receipt table throughout the mixed-version interval. New consumers accept payload batch IDs and fall back to stable transport identity for older messages.
2. Verify every running loyalty replica uses the new processor, including restarted pods and any separate legacy writer deployment. Then deploy Sesame publishers that add stable batch IDs. Confirm every running publisher has moved; old publishers still receive deduplication through transport identity during migration.
3. Observe successful counter commits, stable consumer progress and bounded latency. With Valkey healthy, confirm an immediate replay does not increment twice and an in-progress duplicate retries. Confirm no completion marker appears for a failed SQL transaction, while a successful SQL commit remains acknowledged during Valkey claim/completion failures and emits a throttled warning. Watchtime and payment processing must continue under their existing policies.
4. Verify all running writers, including jobs and any rollback image, have stopped using SQL counter receipts. Take successive read-only metadata/digest snapshots during representative traffic. Require stable insert/update counts and no newer SQL receipt insert/update digest activity. Check instrumentation coverage and resets; an empty digest result alone does not prove there are no writers. Deletion counts may still increase if an old receipt pruner remains.
5. After writer verification and a successful backup, an operator may authorize manual retirement of **only** `bagel_loyalty.counter_batches`. The new consumer stops its legacy receipt writes and pruner; remove any other remaining legacy pruner before cleanup. This PR retains the legacy Ent entity/schema for compatibility, so startup auto-migration can recreate an empty receipt table after a manual drop. Complete separate legacy schema/code retirement before expecting that table to stay absent across restarts. Ensure a rollback does not restart the old receipt writer against a removed table. There is no destructive table drop at application startup, and this task does not drop or truncate anything.

The audited receipt file was approximately 104 MiB; its eventual removal may reclaim that space. Leaving it in place stops neither old writers nor their growth, so writer verification comes first. The separate 2 GiB redo allocation is unaffected. Do not report reclaimed bytes until production metadata and volume measurements confirm them.

## Read-only writer verification

Use an authorized monitoring identity. Save at least two snapshots with their UTC timestamps and compare the insert/update totals and digest `LAST_SEEN` values across a representative interval. Include active counter traffic and all replicas in that interval. Do not reset Performance Schema counters to make the comparison easier.

```sql
SET SESSION MAX_EXECUTION_TIME = 10000;
SET SESSION information_schema_stats_expiry = 0;

SELECT UTC_TIMESTAMP() AS observed_at;

SELECT TABLE_SCHEMA, TABLE_NAME, TABLE_ROWS, DATA_LENGTH, INDEX_LENGTH,
       CREATE_TIME, UPDATE_TIME
FROM information_schema.tables
WHERE TABLE_SCHEMA = 'bagel_loyalty' AND TABLE_NAME = 'counter_batches';

SELECT OBJECT_SCHEMA, OBJECT_NAME, COUNT_INSERT, COUNT_UPDATE, COUNT_DELETE
FROM performance_schema.table_io_waits_summary_by_table
WHERE OBJECT_SCHEMA = 'bagel_loyalty' AND OBJECT_NAME = 'counter_batches';

SELECT SCHEMA_NAME, DIGEST, DIGEST_TEXT, COUNT_STAR, SUM_ROWS_AFFECTED,
       FIRST_SEEN, LAST_SEEN
FROM performance_schema.events_statements_summary_by_digest
WHERE DIGEST_TEXT LIKE '%counter_batches%'
  AND DIGEST_TEXT REGEXP '^(INSERT|REPLACE|UPDATE)'
ORDER BY LAST_SEEN DESC;

SELECT COUNT(*) AS receipts, MAX(created_at) AS newest_receipt
FROM bagel_loyalty.counter_batches;

SHOW GLOBAL STATUS LIKE 'Uptime';
SHOW VARIABLES LIKE 'performance_schema';
```

Table row/page estimates and `UPDATE_TIME` alone are not reliable write detectors. The exact row count can stay constant while inserts and pruning offset each other. Confirm statement/table instrumentation is enabled and that snapshots were not separated by a server restart or summary reset. Inspect digest schema names to distinguish this ledger from a similarly named table elsewhere. If these views are unavailable or incomplete, obtain equivalent writer telemetry before authorizing cleanup.

The design lives in `app/twitch/sesame/engine/loyalty_reporter.go`, `internal/domain/event/data/loyalty_events.go`, `app/db/loyalty/repository/counter_processor.go`, and `app/db/loyalty/main.go`.
