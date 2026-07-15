---
title: New Relic bottleneck plan
description: An efficiency-first plan for tracing and capacity telemetry across Twitch ingress, NATS, projector, Valkey, and database-owning services.
---

## Objective

Make a slow or overloaded event explain itself in New Relic from Twitch ingress through NATS and the projector, while
also distinguishing application work from database-pool, concurrency-gate, broker, Valkey, CPU, and network waiting.
The plan deliberately reuses the agents already running in production and keeps telemetry cardinality and ingest
bounded.

The database-owning services in scope are `users`, `modules`, `commands`, and `transactions`. The projector does not
query MySQL; its datastore is Valkey.

## Existing baseline

Keep and build on the instrumentation that already exists:

- `pkg/monitor` starts one Go APM application per service, enables distributed tracing, and correlates zap logs.
- `pkg/bus` creates one transaction per JetStream delivery and propagates trace headers through the native Go NATS publisher.
- `nrmysql` creates datastore segments for ent queries that carry the transaction context.
- projection Valkey operations create Redis-compatible datastore segments.
- ingress already reports low-volume `Custom/Ingress/*` counters and `IngressEvent` lifecycle events.
- the cluster New Relic bundle already collects Kubernetes, logs, Prometheus data, NATS exporter metrics, and Valkey
  integration metrics.

The missing information is primarily queueing and saturation, not another general-purpose collector.

## Efficiency rules

1. **Reuse APM for code timing and the existing agents for infrastructure.** Do not add OpenTelemetry, Pixie, a
   second Prometheus scraper, or an application metrics server in the first rollout.
2. **Emit state snapshots every 30 seconds, not per-message custom events.** Per-message detail belongs on sampled
   APM transactions and spans. Capacity snapshots belong in one `ServiceCapacitySample` event.
3. **Keep names finite.** Metric, transaction, and span names may contain only enumerated operations, normalized
   subjects, stages, lanes, and outcomes. User IDs, message IDs, stream IDs, pod UIDs, raw errors, and arbitrary NATS
   subjects must never enter a name or facet used by alerts.
4. **Prefer deltas and ratios to duplicate counters.** NATS delivery and Kubernetes resource metrics already exist;
   dashboards should query them rather than republish them from application code.
5. **Instrument boundaries first.** Add an inner span only when a boundary remains opaque after the outer timing is
   visible. This keeps the hot ingress path small.
6. **Gate optional integrations on evidence.** Add server-side MySQL collection only if client pool/query telemetry
   cannot explain observed latency.

## Signal model

### Trace attributes

Use these bounded attributes across Go and Elixir transactions:

| Attribute | Values |
| --- | --- |
| `messaging.system` | `nats` |
| `messaging.operation` | `publish`, `request`, `process`, `reply` |
| `messaging.destination` | normalized configured subject, never an ID-expanded subject |
| `event.type` | the finite Twitch/domain event type set |
| `event.lane` | `premium`, `standard`, `stream` |
| `result` | `ok`, `error`, `timeout`, `dropped`, `deferred`, `invalid` |
| `dependency` | `nats`, `valkey`, `mysql`, `twitch` |
| `cache.result` | `hit`, `miss`, `negative_hit`, `error` |
| `prewarm.branch` | `users`, `modules`, `commands` |

Message IDs may be included on sampled traces and correlated logs for diagnosis, but never on aggregate custom
events, metrics, dashboards, or alerts. Do not send Twitch payloads, chat text, SQL parameter values, credentials, or
tokens.

### Capacity event

Add a small helper to the Go and Elixir monitoring wrappers that reports one event with this shape every 30 seconds:

```text
ServiceCapacitySample
  service                 projector | ingress | users | modules | commands | transactions
  component               dispatcher | db_pool | db_gate
  capacity                configured maximum
  inUse                   current active work
  queued                  current waiting work
  waitCountDelta           waits since the previous sample
  waitMsDelta              cumulative wait milliseconds since the previous sample
  timeoutCountDelta        timeouts since the previous sample
  droppedCountDelta        drops since the previous sample
```

Only fields applicable to the component are set. At a 30-second interval this is two events per component per pod per
minute, which is small beside per-message telemetry and produces directly alertable utilization and wait-rate signals.

## Phase 1: restore trace continuity

This phase has the highest diagnostic value and should ship first.

### Core NATS request/reply

Update `pkg/bus/rpc.go`:

1. In `RequestJSON`, create a `nats.Msg`, call `InsertDistributedTraceHeaders` from the transaction in `ctx`, copy
   those values into `Msg.Header`, and use `RequestMsgWithContext`.
2. In `QueueSubscribeJSON`, copy `msg.Header` into an `http.Header` and call
   `AcceptDistributedTraceHeaders(newrelic.TransportQueue, headers)` before the request is decoded.
3. Add a client segment covering request/reply wait. Keep the segment name normalized to the configured RPC subject.
4. Add tests proving W3C/New Relic headers are injected and accepted without requiring a live New Relic account.

This closes the current ingress/projector-to-data-service and projector-to-outgress RPC trace gaps.

### Ingress notification transaction

Update `Ingress.Dispatcher`, `Ingress.Pipeline`, `Ingress.Nats`, and `Ingress.BroadcasterStatus`:

1. Store `enqueued_at` with the dispatcher item.
2. Start one `NewRelic.other_transaction("Ingress", "Notification")` inside the supervised task. Record dispatcher
   wait as an attribute and custom metric; starting it in the task avoids contaminating the long-lived shard process.
3. Add only four initial spans: `route`, `broadcaster_status.request` on a cache miss, `encode`, and `nats.publish`.
4. Pass `NewRelic.distributed_trace_headers(:other)` through Gnat's existing `headers:` option for both publish and
   request calls.
5. Classify the result and record the finite event type and lane. Notice errors, but do not treat expected filtered
   chat messages as errors.

Gnat 1.15.1 already supports headers for `pub` and `request`, and the installed Elixir agent exposes both transaction
and distributed-trace header APIs, so this requires no dependency change.

### Projector prewarm

The three prewarm goroutines currently use `context.Background()`, so their RPC and Valkey work is invisible to the
stream-event trace. Do not keep the original consumer transaction open for up to five seconds merely to attach
best-effort work.

Instead:

1. Before launching the goroutines, extract distributed headers from the stream-event transaction.
2. Start one background transaction per `users`, `modules`, and `commands` branch, accept the copied parent headers,
   and put that child transaction in the branch context.
3. End each branch transaction after its RPC and Valkey write complete.
4. Add `prewarm.branch` and `result`; keep user ID only in trace/log context.

This preserves the fire-and-forget behavior while producing three causally linked child transactions.

### Phase 1 acceptance

- A synthetic ingress notification yields an ingress transaction followed by the correct Go consumer transaction.
- A broadcaster cache miss includes the users RPC responder and its MySQL segment in the same distributed trace.
- A stream-online trace links the projector consumer and all three prewarm branches.
- Existing local tests still run with no license key and make no network calls.

## Phase 2: expose application saturation

### Ingress dispatcher and cache

Extend `Ingress.Metrics` with a 30-second sampler and cheap monotonic counters. Report:

- dispatcher `running`, queue length, configured limits, queue wait, drops, and abnormal task exits;
- broadcaster cache hits, misses, negative hits, load errors, and current entry count;
- NATS publish/request outcomes and timeouts;
- shard reconnect, zombie-timeout, and notification counts as existing counters, not custom events per notification.

Queue length is read in the dispatcher GenServer and cache size via `:ets.info(table, :size)`. Do not walk either data
structure to produce telemetry.

### Database pool and gate

Add `monitor.ObserveDBPool(ctx, app, service, driver.DB(), 30*time.Second)` in the four database service mains.
The observer samples the standard `sql.DBStats` values:

- `OpenConnections`, `InUse`, `Idle`, and `MaxOpenConnections`;
- deltas for `WaitCount`, `WaitDuration`, `MaxIdleClosed`, `MaxIdleTimeClosed`, and `MaxLifetimeClosed`.

Extend `pkg/db/gate.go` with atomics for current holders, current waiters, cumulative waits, wait duration, and acquire
timeouts. Atomics avoid a second lock on every query. The same 30-second observer reports the pool and gate as two
`ServiceCapacitySample` components.

Keep `nrmysql` as the source of query execution timing. Add repository-level segments only for operations whose
multiple datastore calls remain ambiguous in traces; do not wrap every generated ent method.

### Projector throughput

Do not duplicate NATS exporter consumer metrics. Add application data only for information the broker cannot know:

- handler result and duration by normalized event subject;
- invalid payload drops;
- Valkey write/invalidation outcome;
- prewarm branch result and duration.

Use the existing NATS Prometheus series for durable pending, ack-pending, redelivery, and delivery-rate charts.

### Phase 2 acceptance

- Reducing a DB service pool from four connections to one makes pool utilization and pool/gate wait rise separately
  from MySQL execution time.
- Saturating the ingress dispatcher shows queue depth and queue wait before a drop counter increases.
- Pausing projector consumption raises broker durable lag without falsely reporting slow Valkey writes.
- The capacity sampler stops promptly when its service context is cancelled and creates no goroutine leak.

## Phase 3: New Relic views and alerts as code

Create a small `deploy/newrelic/` Terraform module only after the new attributes have been observed in production.
Keep account ID and user/API keys outside git. Manage the following four dashboards:

1. **Event journey:** ingress accepted/published, NATS rate and lag, projector rate, errors, and Valkey duration.
2. **Ingress pressure:** dispatcher utilization/wait, drops, cache hit ratio, RPC latency, shard reconnects, CPU, and
   memory.
3. **Projector pressure:** durable lag/redelivery, handler percentiles, Valkey segments, prewarm branches, CPU, and
   memory.
4. **Database pressure:** pool/gate utilization and waiting by service beside MySQL datastore duration and service
   resource usage.

Start with alerts that indicate loss or hard saturation:

| Signal | Warning | Critical |
| --- | --- | --- |
| ingress dispatcher utilization | >70% for 10 min | >90% for 5 min |
| dispatcher or NATS publish drops | n/a | >0 for 2 min |
| DB pool utilization with waiters | >80% for 10 min | >95% for 5 min |
| DB gate acquire timeouts | n/a | >0 for 5 min |
| projector durable lag | positive growth for 10 min | oldest pending exceeds the event freshness target |
| RPC/prewarm error ratio | >2% for 10 min | >5% for 5 min |
| required telemetry | n/a | loss of signal for 5 min |

After two weeks of baseline data, replace guessed latency thresholds with anomaly conditions or thresholds derived from
the observed p95/p99. Do not page on cache misses, normal chat filtering, or isolated best-effort prewarm failures.

## Phase 4: evidence-gated MySQL server telemetry

Client telemetry should answer whether time is spent before a connection, waiting at the process gate, or executing a
query. Add server telemetry only when query execution remains the unexplained dominant segment.

Preferred order:

1. Enable the OCI-to-New Relic integration for managed HeatWave service metrics if the existing OCI integration can
   expose the needed connection, CPU, storage, and HeatWave signals with no in-cluster workload.
2. If lock, buffer-pool, thread, or slow-query data is still required, deploy exactly one remote `nri-mysql` runner
   with a TLS-enabled, read-only monitoring account. Do not configure the same remote target on every kubelet
   infrastructure DaemonSet, which would duplicate data four times.
3. Enable only the extended metric sets needed for an active investigation, then measure ingest before retaining them.

## Rollout sequence

Use four independently reversible changes:

1. **Trace propagation:** shared Go RPC, ingress transactions/headers, and projector prewarm linkage.
2. **Capacity samples:** ingress dispatcher/cache and Go DB pool/gate observers.
3. **New Relic as code:** dashboards, loss/saturation alerts, and a single workload grouping the six services.
4. **Optional database integration:** only after Phase 2 evidence satisfies the gate above.

Roll out to one pod or one database service first where the deployment shape permits it. Compare for 24 hours before
fleet rollout:

- service CPU and memory;
- p50/p95 transaction duration;
- New Relic ingest volume by data type;
- agent supportability errors and dropped spans/events;
- trace completeness across NATS.

Proceed when CPU regression is below 2%, memory growth is below 10 MiB per pod, hot-path p95 regression is below 2%,
and projected monthly ingest remains inside the account budget. Otherwise increase sampling intervals, remove the
least useful inner spans, or retain traces only at the boundary level.

## Definition of done

The integration is complete when a controlled test can independently create each bottleneck below and the New Relic
views identify the correct boundary without reading raw logs:

- ingress dispatcher queue saturation;
- NATS/JetStream projector backlog;
- projector Valkey latency;
- database process-gate waiting;
- database pool waiting;
- MySQL query execution latency;
- service CPU throttling or memory pressure;
- dependency timeout or loss of telemetry.

Completion also requires bounded names/facets, no sensitive payload capture, documented NRQL/dashboard ownership,
passing no-license tests, and measured overhead within the rollout limits.
