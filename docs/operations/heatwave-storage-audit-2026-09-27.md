# HeatWave health and storage audit — September 27, 2026

The live database is healthy. Its reported 2.79 GiB is predominantly MySQL infrastructure overhead. The 34 application accounts occupy approximately 95.5 MiB of application table data and indexes, of which 93.4 MiB is counter replay protection. Ordinary application state outside that ledger occupies approximately 2.1 MiB.

This audit queried New Relic's `nri-mysql` and OCI resource events, then used read-only SQL through the existing restricted monitoring and backup accounts. It did not change production configuration, delete records, or rebuild tables. Credential values were never printed or saved in the repository. Checks ran around 04:13–04:18 UTC on September 27 (00:13–00:18 Toronto time).

## Health

| Measurement | Observed result |
| --- | --- |
| Volume usage | 2.79 GiB / 50 GiB; 5.6% |
| Current CPU | 10.3%; approximately 12.0% mean over available resource samples |
| Peak CPU in available resource samples | 42.5% |
| Reported memory used / allocated | 3.15 / 16 GiB |
| Current connections | 69 / 200; 2 running |
| Maximum connected / running threads over 3 hours | 80 / 2 |
| SQL throughput over 3 hours | 34.2 queries/sec mean; 41.5 peak |
| Server mean statement latency | Approximately 0.50 ms |
| Sampled application SQL p95 | Users 1.96 ms; loyalty 1.71 ms; commands 3.90 ms |
| Slow queries | None reported; slow threshold is 10 seconds |
| InnoDB buffer hit ratio | 100% in the observed interval |
| Disk temporary tables | 0/sec over 3 hours |
| Pending disk reads/writes; redo/buffer stalls | Zero in the sampled interval |
| Row locking | No current waits; occasional brief waits, peak sampled wait rate 0.033/sec |
| Connection-capacity errors | Zero |
| HeatWave node | Available; resource health code 0 |
| Collectors | Running, no restarts; successful collection; source lag approximately 87 seconds |
| Backup evidence | Latest off-OCI job succeeded at 00:23 UTC; encrypted gzip dump 13,837,212 bytes; native backup activity approximately 5.7 hours old, no recent failure metric |

The resource collector has only approximately three hours of history at this check. A query requesting 24 hours therefore does not establish a full-day baseline. Observed storage in that window ranged from 2.77 to 2.82 GiB. Backup success/activity is not a restore test.

`Aborted_connects` is high cumulatively, but the host cache attributes almost all of it to handshake errors, with zero recorded authentication errors. The current rate is exactly 0.2/sec. Periodic TCP health checks are a plausible explanation, but the originating component was not verified. This is not evidence of connection saturation.

## Where the storage goes

These measurements describe different layers and must not be added indiscriminately: allocated table pages and table files overlap, while configured redo capacity includes spare files.

| Component | Measured size / interpretation |
| --- | --- |
| Redo-log capacity | 2 GiB, configured by `innodb_redo_log_capacity` |
| Active redo files | 27 × 64 MiB = 1.6875 GiB; five spare files visible; configured total 2 GiB |
| Application data and indexes | 100,139,008 bytes = 95.5 MiB, refreshed metadata |
| `bagel_loyalty.counter_batches` data and indexes | 97,927,168 bytes = 93.4 MiB |
| `counter_batches` physical file | 109,051,904 bytes = 104 MiB |
| All other application data and indexes | 2,211,840 bytes = 2.1 MiB |
| MySQL system tablespace file | 52 MiB |
| Undo files | Four × 16 MiB = 64 MiB |
| Binary logs | Approximately 14.5 MiB at inspection; automatic expiration already 1 hour |
| Other storage | System tablespace, doublewrite, temporary files, service files and filesystem overhead; not completely attributed through the managed SQL interface |

Initial `information_schema.tables` readings were stale and omitted the newly added counter receipt index. Setting session `information_schema_stats_expiry=0` produced the refreshed figures above. Table page statistics remain estimates; physical file sizes and exact row counts provide separate checks. Do not sum each table's `DATA_FREE` for shared tablespaces: the same shared free space can be reported repeatedly.

The redo checkpoint backlog was only about 10 KiB despite the 2 GiB capacity. Redo writes averaged about 8.5 KiB/sec, with a sampled peak of 10.7 KiB/sec over three hours. This supports investigating a smaller redo allocation; it does not establish headroom for future peaks or justify disabling redo logging.

## Counter receipt retention

The receipt table had approximately 541,000 exact rows. The oldest receipt was September 24 at 21:37 UTC; the newest was current. IDs ranged from 22 to 72 bytes and averaged 42.29 bytes. Its schema uses a `VARCHAR(255)` primary key and an index on `created_at`; the index also carries the primary key, so shorter identities can reduce both structures. `VARCHAR(255)` does not reserve 255 characters in every row.

Approximately 236,686 receipts were created on September 26. Recent traffic was approximately 2.2 receipts/sec. If that daily rate persists, eight days corresponds to roughly 1.9 million receipts. Using today's estimated page footprint, this is on the order of a few hundred MiB before physical-file growth overhead; it is a forecast, not a measured plateau.

The live statement digest confirms cleanup is active: 108 executions of indexed, bounded `DELETE FROM counter_batches WHERE created_at < ? ORDER BY created_at LIMIT ?`, affecting zero rows. Historical implementation at commit `55e835b73` defines an eight-day retention window and ten-minute maintenance interval. It explicitly exceeds publisher retry, normal stream retention and seven-day dead-letter replay. Since no receipts are eight days old yet, zero deletions is expected.

The working tree inspected during the initial audit differed substantially from that implementation and did not contain its `batches.go`/`prune.go`. Live SQL evidence verified writes and cleanup, but exact correspondence between the running image and a source commit was not established. The implementation below was subsequently rebased onto current upstream source; running image versions still require verification during rollout.

## Revised recommendation: short-lived Valkey deduplication

The user clarified after the audit that an occasional counter recount is acceptable. That replaces the initial assumption that counter receipts must protect every possible late replay. For these counters, prefer expiring Valkey deduplication markers over the eight-day SQL receipt ledger.

The current source now implements a **five-minute completed-marker TTL**, keyed by broadcaster and a digest of the stable counter batch identity. Sesame assigns a UUID per immutable counter chunk. Old messages without a payload ID use the stable logical transport identity or JetStream stream-sequence fallback, so they also receive deduplication; only calls without any identity bypass it. Loyalty gathers requests for up to 20 milliseconds, at most 128 messages and 8,192 bump entries, and explicitly batches Valkey claims and completion/release writes with `DoMulti`. Its fixed 32 counter handlers bound live admission. See [counter Valkey rollout](counter-valkey-rollout.md) for the design and retirement checks.

The rebased upstream data consumer uses three NAK delays of 5 seconds, 20 seconds and 60 seconds, a 30-second handler deadline and four-second AckWait. Its 85-second total backoff replaces the fixed 3-second schedule found in the initially inspected dirty working tree. The counter publisher retains immutable failed or ambiguously acknowledged publications and retries at subsequent five-second flushes, with five-second attempt timeouts. Its retry abandonment horizon is reduced from one hour to three minutes to leave room inside the five-minute completion policy for normal backoff and bounded processing. Existing stable IDs, payloads, signed-delta limits and system counter guards are preserved. Effective runtime settings and running image versions still need verification during rollout.

Distinguish a short owner-scoped in-progress lease from a completed marker. An in-progress duplicate should retry rather than be acknowledged as completed. Write the completed marker only after SQL commit; release the lease on a definite rollback. A crash or uncertain outcome between SQL commit and the Valkey completion marker can cause a recount, which is the accepted tradeoff. Do not leave a successful-looking marker after a failed SQL write, since that can suppress the real retry and lose the increment. Keep SQL counter updates transactional even though the deduplication marker is no longer in that transaction.

Valkey infrastructure failures are best effort: claim failures allow SQL persistence with throttled warnings. A known successful SQL commit is acknowledged even if receipt completion fails, rather than NACKing a committed window. Explicit in-progress lease collisions still retry; transient or uncertain SQL failures also remain retryable. Cache outages and later redelivery without a saved receipt can recount, consistent with the accepted counter policy.

The historical publisher inspected during the audit could retry the same batch for up to one hour. The rebased implementation keeps that retry queue and stable publication identity while shortening counter abandonment to three minutes. The command-use reporter's retry behavior is unchanged. A five-minute marker intentionally does not protect every late or manual replay. SQL-to-Valkey crashes, Valkey failover/eviction and late replays may recount, as accepted by the user. This change applies only to counter receipts; billing, payments, giveaways and watchtime award replay policies remain unchanged.

At the measured recent rate, five minutes retains roughly 660–820 receipt keys rather than approximately 541,000 SQL rows. Valkey's exact footprint depends on key encoding, allocator, replicas and persistence. Retiring the SQL receipt table after the running writers have migrated can reclaim its approximately 104 MiB file and stop its growth. It does not reclaim the 2 GiB redo allocation.

Application changes are implemented locally; no production deployment, table cleanup or storage reclamation has been performed. Roll out new consumers before publishers, verify every running writer has stopped using SQL receipts, then obtain a successful backup and operator authorization before retiring `bagel_loyalty.counter_batches`. There is no destructive drop at startup. This PR retains the legacy Ent schema for compatibility; auto-migration can recreate an empty table after manual cleanup until a separate schema/code retirement removes it. The following initial recommendations record the stricter replay assumption used before this clarification; their insistence on retaining eight-day counter receipts is superseded by the policy above.

## Initial audit recommendations

1. **Investigate reducing redo capacity from 2 GiB to 512 MiB.** The capacity difference is 1.5 GiB, the largest potential saving. A controlled change could bring current total usage toward approximately 1.3 GiB if other files stay similar and the service reclaims the old allocation. Oracle documents `innodb_redo_log_capacity` as configurable for recent server versions. Verify applicability to this DB system's shape and configuration first; apply through a HeatWave configuration, not `SET PERSIST`. Validate sustained peak load, checkpoint behavior, log waits and latency before reducing further. This changes the working capacity, not the crash-recovery durability guarantee; keep redo enabled and existing durable flush settings.
2. **Reduce receipt creation by aggregating counter events before publication.** One stable batch identity should cover summed deltas across a short interval rather than one receipt per individual event. Preserve the same identity and payload across retries, and commit receipt insertion with its counter changes. For example, if publication is reduced tenfold, receipt growth can fall by a similar factor; achievable reduction depends on the actual producer workload. This addresses activity-driven storage growth without shortening replay protection.
3. **Consider compact receipt keys after batching.** A fixed binary identity or digest can reduce the primary and secondary indexes. Existing IDs are not uniformly UUID-shaped, so blindly converting them with UUID functions is inappropriate. Choose an identity encoding with explicit collision handling or an accepted hash-collision model; ensure old replay messages map to migrated receipts consistently. Estimate the benefit with a representative disposable table before migrating.
4. **Keep the eight-day replay safety window.** Do not delete receipts older than one day simply to save space while seven-day dead-letter replay remains possible. Confirm the ledger reaches a plateau once the first records expire, and expose row counts, file bytes, oldest receipt and pruner progress in monitoring. Current NRI metrics show volume usage but no application-table storage breakdown.

Table compression is a secondary experiment for the approximately 104 MiB receipt file. It cannot remove the dominant redo allocation. Even very good compression of that table saves only tens of MiB today, and would need CPU/write-latency and managed-service compatibility testing. Binary-log transaction compression is similarly low priority: the current logs total only about 14.5 MiB. Backups already use gzip before encryption.

No production tuning was applied. The local OCI API session is expired, so the configuration ID and per-shape eligibility for a custom redo setting were not verified through the control-plane API. NRI and direct SQL health/storage checks succeeded independently of that limitation.

## Read-only queries for subsequent checks

Run against an authorized identity with the required metadata/table permissions. These queries contain no credential values.

```sql
SET SESSION MAX_EXECUTION_TIME=10000;
SET SESSION TRANSACTION READ ONLY;
SET SESSION information_schema_stats_expiry=0;

SELECT table_schema, table_name, table_rows, data_length, index_length
FROM information_schema.tables
WHERE table_schema LIKE 'bagel\_%'
ORDER BY data_length + index_length DESC;

SELECT COUNT(*) AS receipts, MIN(created_at) AS oldest,
       MAX(created_at) AS newest,
       SUM(created_at < UTC_TIMESTAMP() - INTERVAL 8 DAY) AS expired
FROM bagel_loyalty.counter_batches;

SHOW GLOBAL VARIABLES LIKE 'innodb_redo_log_capacity';
SHOW GLOBAL STATUS LIKE 'Innodb_redo_log%';
SHOW BINARY LOGS;

SELECT NAME, FILE_SIZE, ALLOCATED_SIZE
FROM information_schema.innodb_tablespaces
ORDER BY FILE_SIZE DESC LIMIT 20;
```

## References

- [Live HeatWave New Relic dashboard](https://one.newrelic.com/redirect/entity/MzgyMzE3OXxWSVp8REFTSEJPQVJEfGRhOjEzMjIxMTE2)
- [Oracle HeatWave configuration variables](https://docs.oracle.com/en-us/iaas/mysql-database/doc/configuration-variables.html): configurable redo capacity and version conditions.
- [Oracle HeatWave unsupported features](https://docs.oracle.com/en-us/iaas/mysql-database/doc/unsupported-features.html): configuration mechanism instead of global/persisted SQL settings.
- [MySQL redo-log documentation](https://dev.mysql.com/doc/refman/8.4/en/innodb-redo-log.html): active/spare files and capacity management.
- [Oracle Always Free restrictions](https://docs.oracle.com/en-us/iaas/mysql-database/doc/features-mysql-heatwave-service.html): fixed 50 GiB data/log storage.
- [MySQL page compression](https://dev.mysql.com/doc/refman/8.4/en/innodb-page-compression.html): compression does not apply to redo pages.
