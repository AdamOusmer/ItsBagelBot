<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# NATS hubs

Three `nats-server` pods (`nats-0/1/2`, one per worker node) holding every
JetStream stream at R3 in memory, plus a leaf DaemonSet that apps connect to.
The hubs run **stock upstream**, pinned by digest in `nats.yaml`. This file
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
path (a patched broker — see below), or using more than one stream.

## Measured, 2026-09-03

Production hubs, one R3 memory stream with one shared AckNone durable, three
publisher pods and three consumer pods, production retention (32 MiB byte cap),
publishers unpaced, six pull pumps per pod. Identical client tuning in every
row; only the binary changes.

| binary | admitted | e2e p50 | e2e p99 | produced→stored p50 | stored→received p50 |
|---|---|---|---|---|---|
| stock 2.14.6 (what we run) | 87.2k msg/s | 418 ms | 719 ms | 53 ms | 362 ms |
| patched 2.14.6 | 123.2k msg/s | 43 ms | 81 ms | 37 ms | 4.8 ms |
| patched 2.14.4 | 141.6k msg/s | ~50 ms | 77 ms | 39 ms | ~10 ms |

Note the last two rows against each other: 2.14.6 is ~13% slower than 2.14.4
under the same patches, because 2.14.6 takes the stream lock once per message
in the apply path where 2.14.4 took it once per batch. That is upstream's
regression, not ours, and it is not a reason to pin an older release.

## Why we run stock anyway

The patched build existed briefly and was rolled back on purpose. Carrying a
17-file patch against the broker means re-applying it at every upstream
release, with no support for the patched paths, and the throughput it buys is
not what our users feel: the premium and standard lanes are what keep a paying
broadcaster's commands fast, and those are a routing decision, not a broker
build. If a lane ever needs more than one stream's worth of ingest, split the
subject space across streams — that is the supported answer and it scales past
anything a patch can win.

The patch set lives in this repository's history (PRs #778, #780, #785,
reverted in #787) if the measurement ever needs repeating.

## What actually fixed the latency, and still applies

None of this needs a patched broker; all of it is in `pkg/bus` and the service
manifests today.

- **Bound the publish queue** (`NATS_PUBLISH_QUEUE_SIZE`, sesame runs 512). The
  16,384-slot default parked up to 61k messages per pod — 1.5 s of latency no
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

## Reproducing

`deploy/k8s/bus-bench` is the rig: `-mode setup|publish|consume|cleanup`, and it
reports the latency split at the broker's store timestamp
(`bus.Message.StoredAt`), which is what separates publisher queueing from
consumer lag. Watch for two traps: an interrupted run leaves its publishers
alive inside the bench pods, and a bench consumer that gets OOM-killed silently
depresses the numbers.
