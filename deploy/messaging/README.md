<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# NATS hubs

Three `nats-server` pods (`nats-0/1/2`, one per worker node) holding every
JetStream stream at R3, plus a leaf DaemonSet that apps connect to. The hot
event lanes are in memory; the data, dead-letter and system streams are on file
storage (`pkg/bus/streams.go`). Hub and leaf authenticate in NATS operator (JWT)
mode (`nats-operator.conf`). The hubs run **stock upstream**, pinned by digest
in `nats.yaml` (currently 2.15.0). This file
records what the broker can actually do, because the number is lower than it
looks and the way past it is not a broker setting.

## What one stream can do

A stream's ingest loop and its apply loop hand one lock back and forth, so a
single stream is one serialized path no matter how many cores the pod has. A
3 s execution trace on the leader at ~150k msg/s: ingest running 47.8% of wall
time, apply 51.1%, both at once 1.1%, union 97.8%. The pod was using 2.9 of its
5 cores.

That is the ceiling. More publishers, more connections, jumbo frames, disabling
route compression and moving publishers onto the leader's own node all leave it
where it is. Two things do move it: making each message cheaper inside that
path (a patched broker, see below), or using more than one stream.

## Measured, 2026-09-03

Production hubs, one R3 memory stream with one shared AckNone durable, three
publisher pods and three consumer pods, production retention (32 MiB byte cap),
publishers unpaced, six pull pumps per pod. Identical client tuning in every
row; only the binary changes.

| binary | admitted | e2e p50 | e2e p99 | produced→stored p50 | stored→received p50 |
|---|---|---|---|---|---|
| stock 2.14.6 (what we ran then) | 87.2k msg/s | 418 ms | 719 ms | 53 ms | 362 ms |
| patched 2.14.6 | 123.2k msg/s | 43 ms | 81 ms | 37 ms | 4.8 ms |
| patched 2.14.4 | 141.6k msg/s | ~50 ms | 77 ms | 39 ms | ~10 ms |

Note the last two rows against each other: 2.14.6 is ~13% slower than 2.14.4
under the same patches, because 2.14.6 takes the stream lock once per message
in the apply path where 2.14.4 took it once per batch. That is upstream's
regression, not ours, and it is not a reason to pin an older release.

## Measured, 2026-10-11 (stock 2.15.0)

Production hubs, TWITCH_INGRESS_RETRY at R3 with production retention (32 MiB,
2m) unless noted, one shared R3 durable, three publisher pods and three
consumer pods (one per node), six pull pumps of 250, 735 B payload, 30 s per
cell. Pull rows use the canary subject so they take the hot-lane path (AckNone
pull, as sesame runs); explicit rows take the push lane every other durable
uses. No run lost or duplicated a message unless the row says so.

| shape | offered | admitted | e2e p50 | e2e p99 | produced→stored p50 | stored→received p50 |
|---|---|---|---|---|---|---|
| pull AckNone (production) | 40k | 40.0k | 22 ms | 46 ms | 6.5 ms | 13 ms |
| + max_ack_pending 4000 | 40k | 40.0k | 28 ms | 56 ms | 9.8 ms | 15 ms |
| AckAll, max_ack_pending 4000 | 40k | 40.0k | 854 ms | 1.39 s | 8.2 ms | 846 ms |
| AckAll, max_ack_pending 200000 | 40k | 40.0k | 30 ms | 62 ms | 7.4 ms | 21 ms |
| pull AckNone, duplicate window 1 s | 40k | 40.0k | 25 ms | 272 ms | 7.2 ms | 15 ms |
| pull AckNone, 400 B payload | 40k | 39.9k | 19 ms | 41 ms | 9.5 ms | 8.9 ms |
| pull AckNone, one consumer pod | 40k | 40.0k | 26 ms | 81 ms | 7.6 ms | 17 ms |
| pull AckNone, 1 GiB / 10 s retention | 40k | 39.9k | 17 ms | 122 ms | 8.0 ms | 9.0 ms |
| explicit push, max_ack_pending 20000 | 40k | 39.9k | 25 ms | 108 ms | 7.0 ms | 16 ms |
| explicit push, max_ack_pending 4000 | 40k | 39.9k | 6.6 s | 11.0 s | 8.3 ms | 6.6 s |
| pull AckNone (production) | unpaced | 46.0k | 299 ms | 399 ms | 239 ms | 61 ms |
| pull AckNone (production), other mode | unpaced | 69.3k | 5.2 s | 8.2 s | 134 ms | 5.1 s |
| pull AckNone, 1 GiB / 10 s retention | unpaced | 57.8k | 307 ms | 736 ms | 184 ms | 100 ms |
| AckAll, max_ack_pending 4000 | unpaced | 40.0k | 2.21 s | 2.46 s | 249 ms | 1.96 s |
| pull AckNone, one consumer pod | 80k | 54.2k | 1.68 s | 1.99 s | 172 ms | 1.51 s, 260k evicted unread |
| explicit push, max_ack_pending 20000 | 80k | 56.6k | 14.3 s | 21.5 s | 169 ms | 14.1 s, 428k evicted unread |

What it says:

- **max_ack_pending is a fleet-wide cap on one shared durable, not a per-pod
  one**, and it only exists under AckAll or AckExplicit. Under AckAll it pinned
  ack_pending at 4000 and the floor ack every 250 ms could not free it fast
  enough; on the explicit lane it held the backlog in the stream.
  Both made latency worse by 30x or more. Sesame's `NATS_PULL_MAX_ACK_PENDING`
  is inert today because the hot lanes run AckNone.
- **AckAll buys nothing over AckNone** at equal load and is not a default.
- **The duplicate window costs nothing**: the publisher has not sent
  `Nats-Msg-Id` since the replay dedup dependency was removed, so a 1 s window
  left p50 where the 2m one has it.
- **The ceiling is admission, not delivery.** Unpaced, the hubs admitted 45-46k
  msg/s with ~240 ms of publisher queueing and ~60 ms of delivery; with 1 GiB / 10 s
  retention instead of the 32 MiB cap the unpaced run admitted 57.8k.
  Some runs instead admitted 68-74k and the three consumers (25-65 us
  of CPU per message each) fell 2-12 s behind; that backlog sat in hub memory
  (RSS to 6.4 GiB), mostly in neither num_pending nor num_ack_pending.
- **At production retention a lagging consumer loses messages instead of
  queueing them**: one consumer pod, AckAll, or the explicit lane at 80k evicted
  244-428k messages unread with nothing reporting it.

The record above (87.2k on 2.14.6) does not reproduce on 2.15.0 in this shape.

## Why we run stock anyway

The patched build existed briefly and was rolled back on purpose. Carrying a
17-file patch against the broker means re-applying it at every upstream
release, with no support for the patched paths, and the throughput it buys is
not what our users feel: the premium and standard lanes are what keep a paying
broadcaster's commands fast, and those are a routing decision, not a broker
build. If a lane ever needs more than one stream's worth of ingest, split the
subject space across streams. That is the supported answer and it scales past
anything a patch can win.

The patch set lives in this repository's history (PRs #778, #780, #785,
reverted in #787) if the measurement ever needs repeating.

## What actually fixed the latency, and still applies

None of this needs a patched broker; all of it is in `pkg/bus` and the service
manifests today.

- **Bound the publish queue** (`NATS_PUBLISH_QUEUE_SIZE`, sesame runs 512). The
  16,384-slot default parked up to 61k messages per pod, 1.5 s of latency no
  caller could see. Bounding it turned a 1.4 s p50 into tens of milliseconds
  and cost no throughput. Use at least two cohorts' worth: a queue equal to one
  cohort starves the batcher on a paced source.
- **`NATS_PUBLISH_ACK_WAIT`** (sesame runs 5s). An expired commit wait is an
  at-most-once loss: the broker may have committed, the client refuses to
  replay, and a whole cohort disappears into a log line. It must sit above the
  worst commit tail.
- **`NATS_ATOMIC_ACK_FIRST=true`.** With it off, commits get no timely reply
  under load; commit p99 reached 2.7 s and cohorts were lost outright.
- **Consumer pumps.** Above ~130k msg/s one pull pump per pod cannot keep up and
  the backlog, not the stream, sets end-to-end latency. Six pumps per pod moved
  admission from 152k to 168k in the no-eviction shape.
- **Size retention above the consumers' peak backlog.** At 150k msg/s offered,
  a 32 MiB cap holds 0.67 s of stream: messages were evicted before the durable
  read them (stored→received p50 940 ms) and nothing reported it. Retention is
  a correctness setting before it is a memory setting.

## Two upstream bugs worth filing

Both still present in 2.14.6, both were part of the patch set:

1. `memStore.storeRawMsg` copies the header and body once into local variables
   and again into the message buffer two lines later. The first copy is dead:
   11% of all allocations on the leader at 127k msg/s. It sits directly under
   an existing `TODO(dlc) - Maybe be smarter here`.
2. `getMessageSchedule` runs before the `allowMsgSchedules` check that would
   discard its result, so a stream that never allows schedules parses one on
   every message (~0.4 s per 10 s at 127k msg/s).

## Client distribution after a hub roll

BUS clients dial the `nats` Service only and ignore the pod IPs hubs advertise
in `connect_urls`, so every reconnect goes through Kubernetes readiness and the
node-local preference. After a rolling restart the last hub to come back still
holds few clients until clients reconnect for their own reasons; on 2026-10-11
the 227 BUS connections sat 0 / 5 / 222 across nats-0/1/2. Read the count per
hub with `/connz?limit=1024` on each pod. Lame duck (`nats-server --signal ldm`)
empties a hub: a maintenance tool, not a balancer.

## Reproducing

`deploy/k8s/bus-bench` is the rig: `-mode setup|publish|consume|cleanup`, and it
reports the latency split at the broker's store timestamp
(`bus.Message.StoredAt`), which is what separates publisher queueing from
consumer lag. Watch for two traps: an interrupted run leaves its publishers
alive inside the bench pods, and a bench consumer that gets OOM-killed silently
depresses the numbers.
