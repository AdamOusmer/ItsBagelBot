---
title: Performance and cleanup roadmap
description: Evidence-gated latency, NATS R3, Valkey, and 20,000-channel capacity work after the native data-plane migration.
---

## Objective

Certify the bot for 20,000 channels without hiding a latency regression behind a
throughput number. Work proceeds in measurement order: make the full path
explainable, qualify NATS R3, tune the real Valkey commands, then run the
production-shaped soak. Sharding and additional hardware are capacity-triggered
decisions, not prerequisites.

## Baseline that must not be conflated

| Path | Verified result | Meaning |
| --- | ---: | --- |
| R1 ingress JetStream | 123,834 events/s, 3M acknowledged, zero errors | Current throughput ceiling with deduplication off and leader-direct publishing |
| R3 JetStream | 11,996.6 events/s for 60s, 13.681 ms fleet p99 | Stable at the tested rate, but not latency-qualified for the old 2 ms JetStream target |
| R3 `s2_fast` calibration | About 75,000 events/s, zero final lag | Best retained high-load calibration; it is not a 90k/30-minute or 2 ms p99 qualification |
| Sesame engine + input, memory output | 20,074 commands/s, 67 us p95 | The command engine is not the present bottleneck |
| Sesame full confirmed-output fleet | 22,971 commands/s | Worst case where every input emits an output; NATS confirmation dominates |
| Valkey node-local reads | 0.247-0.790 ms p99 | Already inside the 2 ms read SLO on every node |
| Valkey primary writes | 2.416-10.207 ms p99 | Physical distance to the elected primary determines the tail |

R1 and R3 are different products. The 123k R1 result must never be reported as
R3 capacity, and an R3 throughput pass must never override a failed latency gate.

## Phase 1: full-path latency attribution

Instrument bounded stage names across the complete internal command journey:

1. ingress dispatcher wait;
2. filter and route;
3. JSON encode;
4. NATS publish and JetStream acknowledgement;
5. consumer delivery wait;
6. Sesame decode and engine execution;
7. Valkey and RPC dependency time;
8. output encode and confirmed publish;
9. outgress processing.

Use distributed trace headers on core NATS RPC and JetStream messages. Aggregate
names may contain only configured subjects, finite stages, lanes, operations,
and outcomes. IDs remain sampled trace/log attributes and never become metric
names or dashboard facets.

Phase 1 passes when a controlled command workload reports p50, p95, p99,
minimum, and maximum for every stage without reconstructing the path from raw
logs.

## Phase 2: NATS R3 qualification

The architecture is fixed during this phase:

- hubs own JetStream and leaves serve RPC only;
- broker deduplication remains off;
- publishers dial the node-local hub Service;
- worker1 participates as a replica but is not the preferred leader;
- no stream partitioning or sharding;
- production streams remain unchanged during isolated qualification.

Run the same isolated stream across a matrix of R1/R3, leader-local/non-leader,
payload size, publish mode, cohort size, and connection count. Capture PubAck
percentiles beside server CPU, route pending bytes, follower lag, reconnects,
retransmits, and leader changes. Change one server setting at a time and retain
it only when a repeat run improves the result.

The desired R3 operating point is 90,000 events/s for at least 30 minutes with
zero errors and zero final follower lag. It is not promotable unless its loaded
latency target is explicitly accepted. The historical 2 ms p99 remains the
normal-load JetStream SLO; the current hardware evidence shows that a
fleet-wide R3 p99 near 14 ms may be the physical worker1 floor.

If 90k throughput and the accepted latency cannot coexist, record the lower
stable ceiling. Do not weaken an automated gate merely to make the run pass.

## Phase 3: Valkey hot commands

Benchmark the exact Sesame, projector, and outgress commands rather than generic
GET/SET traffic. Separate the same-node replica read path from Sentinel-routed
primary writes.

Priorities are:

1. keep TLS connections persistent and pooled;
2. remove transport replay claims from Valkey while retaining domain cooldown,
   timer, loyalty, live-state, and reputation data;
3. pipeline independent cold reads;
4. collapse related rate-limit state into one atomic server operation;
5. serve frequently-read global state from a versioned local snapshot repaired
   by Valkey and NATS invalidation;
6. measure snapshot/failover pauses before changing persistence settings.

The local read p99 target is 2 ms and is already met. Primary writes are judged
against a topology-aware target: a remote write tail is not justification for
enabling replica writes that can be lost during resynchronization.

## Phase 4: 20,000-channel certification

After phases 1-3, run a six-hour isolated soak with realistic online ratios,
chat rates, command bursts, raids, one NATS member restart, one Valkey failover,
and one Sesame replacement. Ingress remains one pod per NATS node and does not
add WebSocket shards below 75% occupancy.

Capacity is calculated from the measured event rate, not the channel count:

```text
safe events/s = verified sustained ceiling * 0.70
safe channels = safe events/s / measured peak events/s per channel
```

At a verified 90k ceiling, the operating budget is 63k events/s. Twenty
thousand channels fit while the measured simultaneous peak stays below 3.15
events/s per channel.

## Expansion gates

Do not shard NATS, add a server, replace Valkey, or migrate the CNI until either
the verified operating envelope exceeds 70% for 15 minutes or a latency SLO
fails after application and server tuning. Any expansion proposal must include
the triggering telemetry, the expected headroom, cost, rollback, and a repeatable
acceptance run.

## Cleanup that travels with the phases

- move the Tailscale operator OAuth credential into Doppler/Flux ownership and
  rotate it;
- remove stale Watermill, Linkerd, Tailscale-data-plane, and Valkey replay-dedup
  documentation;
- make every benchmark clean its stream, consumers, pods, temporary ACL, and
  result objects after success, failure, or interruption;
- render Helm values and resource requests in CI so ignored chart keys fail
  before production;
- keep benchmark results dated and immutable, with one current capacity profile
  as the source of truth.
