# Shared Elixir Valkey retained stream

`Platform.Valkey` owns the Redix connection boundary; configure the named connection
with authentication, TLS and certificate verification as appropriate for the
deployment. Failures return `{:error, :unavailable}` or
`{:error, {:command, message}}`, including per-entry pipeline results, so
callers never see Redix structs. `RetainedStream` atomically stores ordered binary records, two
45-second renewable leases, and one expiring one-use migration reference per
stream. Sequence numbers are exact decimal strings through the full uint64
range. The stream record is the authoritative head; a retained record retried
with identical bytes succeeds, while a conflicting retry fails. A retry after
the original record has been trimmed cannot be verified and fails. A head-tagged byte count makes normal append accounting constant time; when
the cache is absent or stale after an interrupted write, it is rebuilt by scanning
retained records. At most 128 records are trimmed per append; larger cleanup
needs return backpressure. Retention is
bounded by count and bytes; append returns `full` when records cannot be safely
removed. A live lease pins its applied cursor until an explicit checkpoint, close, or expiry;
reads renew the lease but do not assert application delivery. Migration freezes the old
lease at its cutoff; an unconsumed token expires and unfreezes it, and a
consumed token keeps it frozen only while the receiving lease is live. The
receiving lease starts reading after the cutoff.

Run the integration test against an isolated Valkey server with
`VALKEY_TEST_PORT=<port> mix test`. Production durability and cross-pod
correctness require a shared, persistent Valkey deployment configured without
key eviction, with appropriate failover and persistence. The configured Valkey
persistence and replication policy determines crash durability; this package
does not promise lossless recovery from a server crash. These primitives do not
create or publish domain events on their own.

`ManagedRecords.Policy.new!/1` validates a bounded schema, initial fields,
state names, sticky reasons, and retention durations once at construction.
Callers declare their own keys and policy; the shared package owns the atomic
scripts. Single operations use precomputed `EVALSHA` with `NOSCRIPT` fallback.
The bounded admission pipeline uses fixed-script `EVAL` to preserve ordering
even if a script cache is flushed while a batch is in flight. Admission passes
only its required policy fields as arguments, without JSON decoding per event.
