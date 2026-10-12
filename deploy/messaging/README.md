<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# NATS hubs

Three `nats-server` pods (`nats-0/1/2`, one per worker node) holding every
JetStream stream at R3, plus a leaf DaemonSet that apps connect to. The hot
event lanes are in memory; the data, dead-letter and system streams are on file
storage (`pkg/bus/streams.go`). Hub and leaf authenticate in NATS operator (JWT)
mode (`nats-operator.conf`). The hubs run nats-server 2.15.1 with our patch set
(`deploy/messaging/nats-server`), pinned by digest in `nats.yaml`; the leaves
run stock 2.15.0. This file
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

## Where one stream's time goes, 2026-10-11 (stock 2.15.0)

Same hubs and bench lane as above, record shape (3 publisher pods x 4
connections, 3 consumer pods x 6 pumps of 250, 735 B payload), 30 s cells.
The rig now reconciles every sequence by count, sum and xor across all pods
(tail loss and cross-pod duplicates included), checks `Flush`, and splits
latency at the broker's stamp, which is taken **before** the RAFT proposal:
`produced→proposed` is publisher queueing plus staging, `proposed→received`
includes commit and apply. `produced→committed` is the terminal `CommitMsg`
ack of a sampled cohort. No run below lost or duplicated a message.

What the profiles and `/raftz` show:

- **One RAFT index is one cohort** (1.886M messages over 7,383 indexes). Under
  unpaced load the stream leader's committed−applied sat at 188-190 indexes
  for the whole run, exactly the publisher fleet's in-flight window (12
  workers x 16 slots). Followers stayed at 0-50. The commit ack waits for the
  leader's apply, so `produced→committed` (~900 ms) ran four to six times
  `e2e` (~160 ms) whenever the durable's leader was on a follower.
- **Ingest and apply alternate on `mset.mu`.** A 3 s execution trace of the
  leader: apply running 62.8%, ingest 39.5%, running together 4.1%, union
  98.2%. That serial path is the ceiling. The pod used 3.2-3.9 of its 5
  cores with no cgroup throttling; TLS writes and syscalls are a quarter of
  the CPU but are not what binds.
- **Inline eviction is what makes the durable's placement matter.** When
  `MaxBytes` or `MaxMsgsPer` binds, every store evicts inside the apply
  goroutine (`memStore.enforceBytesLimit`, `enforcePerSubjectLimit`), and each
  single-message removal calls `consumer.decStreamPending`, which takes the
  durable's `o.mu`. On the server that leads the durable, its delivery loop
  holds `o.mu` per message. Trace of the colocated case: apply blocked 13% in
  `decStreamPending`, ingest blocked 31-32% behind it on `mset.mu`. With the
  durable's leader on a follower, the same contention moves to that
  follower: its stream apply fell up to 1,034 indexes (about 4 s) behind
  while the other follower stayed at 0-3, and delivery fell behind with it.
  That is the "69k admitted, seconds of consumer lag" mode, and colocation
  is the 45k mode. Age expiry removes on the store's timer goroutine instead,
  outside apply and `mset.mu`.

Round 1, 32 MiB / 2 m (bytes bind), durable leader seated by recreating it:

| cell | admitted | e2e p50/p99 | produced→committed p50 | leader apply lag |
|---|---|---|---|---|
| separated, unpaced (5 reps) | 62.0-67.1k | 156-846 / 234-1567 ms | 865-942 ms | 189-307 |
| colocated, unpaced (2 reps) | 41.8k, 42.7k | 325 / 465 ms | 364 ms | 10-13 |
| colocated, 60k offered | 40.5k | 337 / 464 ms | 374 ms | 8 |
| separated, 60k offered | 59.9k | 21-22 / 42-65 ms | 15-20 ms | 17-37 |
| header +64 B, separated unpaced | 64.1k | 152 / 250 ms | 899 ms | 190 |
| batch 512, separated unpaced | 66-67k | 200-1873 ms p50 | 1.6 s | 382-530 |
| 8 publisher connections | 62.8k | 362 / 604 ms | 1.79 s | 382 |
| ACK_FIRST off | 66.5-67.0k | 350-499 / 995-1432 ms | 850 ms | 191-258 |

Round 2, 128 MiB cap, same message rate, only the binding limit changes:

| cell | 10 s age (bytes bind) | 1 s age (age binds) |
|---|---|---|
| colocated, unpaced | 40.1k, e2e 346 / 577 ms | 59.5k, e2e 242 / 365 ms |
| colocated, 60k offered | 39.8k, e2e 364 / 519 ms | 59.9k, e2e 47 / 181 ms |
| separated, unpaced (2 reps) | 63.3-63.6k, e2e 194-2605 / 460-4466 ms (follower apply lag up to 1,015) | 66.0-69.6k, e2e 146-149 / 274-383 ms |
| separated, 60k offered | 59.5k, e2e 477 / 2057 ms (lag 880) | 59.9k, e2e 19 / 114 ms |

In-flight cohorts, 1 s age, separated, three alternating pairs: 16 slots
admitted 67.4-69.3k with `produced→committed` p50 825-860 ms (p99 987-1042),
leader backlog 188; 8 slots admitted 68.5-68.8k at 481-496 ms (p99 633-694),
backlog 92-94. At 60k offered 8 slots had the wider e2e p99 of the two pairs
(82-130 ms against 66-88 ms) at the same p50.
Pull batch 1000 admitted 71.7k here, but in round 3 its `proposed→received`
was 190-275 ms against 57-81 ms; 2 pumps lost 5%; a 1-replica durable
(measured, not shipped) admitted 69.2k, no better than R3.

Round 3, production premium-lane shape (384 MiB, `MaxMsgsPer` 400k, one
subject). Before: 10 s age, 16 in-flight. After: 6 s age, 8 in-flight.

| cell | before | after |
|---|---|---|
| colocated, unpaced | 46.4k, e2e 332 / 531 ms | 62.2k-63.5k, e2e 227-231 / 504-506 ms |
| colocated, 60k offered | 45.3k, e2e 331 / 478 ms | 59.8k, e2e 26 / 226 ms |
| separated, unpaced | 64.3-66.7k, e2e 674-860 / 1115-1313 ms, commit 870 ms | 65.5-68.1k, e2e 210-254 / 444-727 ms, commit 484-494 ms |
| separated, 60k offered | 59.9k, e2e 35-86 / 139-356 ms | 59.7-59.9k, e2e 19-54 / 226-275 ms |

After, by offered rate: 40k gives e2e 20 / 48 ms separated and 21 / 35 ms
colocated; 50k gives 17 / 55 and 23 / 97 ms; 55k gives 20 / 134 and 170 /
411 ms. The hubs sat at 2.3-3.4 GiB RSS in every 384 MiB cell (GOMEMLIMIT
3584 MiB), which is where the p99 tails come from. So one stream on these
hubs holds **50k msg/s with p99 under 100 ms wherever the durable's leader
lands**, and admits 62-68k unpaced. Before, a colocated durable capped the
same lane at 45-46k.

Leader top frames, colocated unpaced, before → after: `Syscall6` 22.0% →
17.4%, `memmove` 6.1% → 9.0%, s2 encode 3.7% → 4.3%; `enforcePerSubjectLimit`
4.2% → gone, `expireMsgs` (timer goroutine) → 4.5%; ingest blocked on
`mset.mu` 32% → 19%; apply no longer blocks in `decStreamPending`.

Not shipped:

- **Keeping the durable's leader off the stream leader.** Measured above by
  seating, it is worth 46k → 62k on its own. A watcher would need
  `$JS.API.CONSUMER.LEADER.STEPDOWN` for the lane streams on `worker_bus`, a
  single coordinator rather than every pod, and a transfer test under load:
  an AckNone R3 durable resumes at its replicated delivered sequence, so a
  transfer can replay or skip what the old leader sent after its last
  replicated state.
- **More retention bytes.** 640 MiB / 10 s admitted 68.5-72.7k, but the hubs
  reach GOMEMLIMIT. Shorter age is the memory-neutral way to the same effect.
- Header trimming (+64 B of header moved nothing), larger cohorts (+5%
  admitted, twice the commit latency), more publisher connections, ACK_FIRST
  off, and a 1-replica durable: none beat noise.

## Measured, 2026-10-12 (patched 2.15.1-bench)

Production hubs on the patched build (`deploy/messaging/nats-server`, fork tag
`bagel/v2.15.1-bench`, the `NATS_BENCH_*` knobs in `nats.yaml`, eviction
batching off), same rig and lane as 2026-10-11: TWITCH_INGRESS_RETRY at R3, one
shared R3 AckNone pull durable on the canary subject, three publisher pods with
8 in-flight cohorts, three consumer pods with six pumps of 250, 735 B payload.
60 s cells, each run twice; ranges cover both runs. Colocated seats the
durable's leader on the stream leader, separated seats it on a follower. Loss
is admitted minus received after reconciling every sequence; no run duplicated
a message, no hub restarted and every stream and consumer stayed current.

| shape | placement | offered | admitted | loss | e2e p50 / p99 ms | commit p50 / p99 ms | apply lag peak | hub RSS peak MiB | leader cores |
|---|---|---|---|---|---|---|---|---|---|
| 32 MiB / 2 m | separated | 60k | 60.0k | 0, 0 | 17-20 / 55-58 | 12-15 / 61-65 | 8-39 | 856-977 | 2.60-2.67 |
| 32 MiB / 2 m | separated | 70k | 70.0k | 0, 0 | 13 / 28-30 | 10-12 / 29-34 | 9 | 881-903 | 2.70-2.89 |
| 32 MiB / 2 m | separated | 75k | 75.0k | 0, 0 | 13-15 / 35-36 | 10-11 / 30-52 | 8-36 | 934-2885 | 2.74-2.83 |
| 32 MiB / 2 m | separated | 80k | 80.0k | 0, 0 | 13-14 / 118-184 | 11-12 / 39-306 | 60-94 | 981-1141 | 3.04-3.08 |
| 32 MiB / 2 m | separated | 100k | 93.3k-96.2k | 0, 6443 | 105-119 / 195-418 | 306-348 / 459-529 | 93-94 | 1295-1333 | 3.30-3.32 |
| 32 MiB / 2 m | separated | 120k | 93.4k-101.9k | 42137, 0 | 106-114 / 259-398 | 323-357 / 480-502 | 92-94 | 1263-1309 | 3.31-3.33 |
| 32 MiB / 2 m | separated | unpaced | 93.9k-98.9k | 0, 0 | 106-112 / 203-220 | 324-360 / 502-571 | 93 | 1329-1332 | 3.34-3.36 |
| 32 MiB / 2 m | colocated | 50k | 50.0k | 0, 0 | 14-20 / 28-34 | 12-17 / 28-31 | 6-8 | 855-863 | 2.56 |
| 32 MiB / 2 m | colocated | 55k | 54.9k-55.0k | 0, 0 | 18 / 27-58 | 15-16 / 26-30 | 6-7 | 908-920 | 2.68-2.72 |
| 32 MiB / 2 m | colocated | 60k | 60.0k | 0, 0 | 25-134 / 350-351 | 19 / 245-342 | 10-14 | 996-1020 | 3.19-3.26 |
| 32 MiB / 2 m | colocated | 80k | 67.7k-69.7k | 0, 0 | 195-200 / 278-302 | 202-218 / 322-336 | 11-13 | 1058-1101 | 3.30-3.31 |
| 32 MiB / 2 m | colocated | 100k | 68.0k-69.9k | 0, 0 | 195-200 / 275-276 | 214-221 / 326-334 | 11-17 | 1133-1179 | 3.29-3.30 |
| 32 MiB / 2 m | colocated | 120k | 68.9k-73.5k | 0, 0 | 186-190 / 268-345 | 206 / 308-428 | 12-13 | 1173-1309 | 3.36 |
| 32 MiB / 2 m | colocated | unpaced | 69.0k-76.1k | 0, 0 | 178-199 / 279-305 | 199-216 / 328-366 | 10-15 | 1135-1228 | 3.33-3.34 |
| 384 MiB / 6 s / 400k | separated | 50k | 50.0k | 0, 0 | 15-16 / 36-44 | 12 / 40-44 | 5-6 | 2202-2806 | 2.02-2.04 |
| 384 MiB / 6 s / 400k | separated | 80k | 80.0k | 0, 0 | 14-18 / 33-53 | 12-14 / 31-72 | 52-87 | 2838-2918 | 2.98-3.02 |
| 384 MiB / 6 s / 400k | separated | 100k | 96.2k-99.5k | 0, 0 | 98-106 / 177-488 | 15-256 / 423-453 | 92-93 | 3160-3180 | 3.34-3.37 |
| 384 MiB / 6 s / 400k | separated | unpaced | 98.7k-99.4k | 0, 0 | 103-111 / 195-348 | 339-342 / 463-500 | 93-94 | 3181-3278 | 3.35-3.38 |
| 384 MiB / 6 s / 400k | colocated | 50k | 50.0k | 0, 0 | 15-18 / 37-40 | 13-15 / 32-34 | 6-10 | 2285-2304 | 2.49-2.53 |
| 384 MiB / 6 s / 400k | colocated | 60k | 59.8k-59.9k | 0, 0 | 18-26 / 113-314 | 12-14 / 77-224 | 6-7 | 2659-2890 | 2.97-3.15 |
| 384 MiB / 6 s / 400k | colocated | 80k | 71.1k-72.4k | 0, 0 | 187 / 274-294 | 145-166 / 310-346 | 10-14 | 3050-3103 | 3.34-3.35 |
| 384 MiB / 6 s / 400k | colocated | 100k | 72.1k-72.5k | 0, 0 | 188-194 / 276-305 | 201-205 / 347-369 | 10-18 | 3079-3105 | 3.32-3.34 |
| 384 MiB / 6 s / 400k | colocated | unpaced | 72.5k-75.7k | 0, 0 | 184-194 / 270-272 | 199-211 / 305-314 | 12-14 | 3019-3104 | 3.31-3.35 |

Against stock 2.15.0 on the same hubs:

- **The ceiling moved by half.** Unpaced, one stream admitted 94-99k msg/s with
  the durable separated (was 62-67k) and 69-76k colocated (was 42-46k at 32 MiB,
  62-64k in the premium shape). The stream leader still sat at 3.3-3.4 of its 5
  cores at the ceiling: the same serialized path, each message cheaper.
- **Clean rate (p99 under 100 ms, zero loss)**: 75k separated and 55k colocated
  at 32 MiB; 80k separated and 50k colocated in the premium shape, where 60k
  colocated reached a 113-314 ms p99. So one stream holds **50k msg/s wherever
  the durable's leader lands**, as before, and 75-80k once it sits on a
  follower. Colocation, not the binary, still sets the floor.
- **At 50k** e2e is 15-18 ms p50 and 36-44 ms p99 (was 17-23 / 55-97 ms).
- **32 MiB still loses messages under consumer lag.** Separated at 100k and 120k
  offered, two of four runs evicted 6.4k and 42k messages unread. The premium
  shape lost nothing, unpaced included.
- **Memory is unchanged where it binds.** Premium-shape cells peaked at 2.2-3.3
  GiB hub RSS (was 2.3-3.4 GiB); 32 MiB cells at 0.86-1.3 GiB (the 2.9 GiB in
  the 75k row is heap left over from the premium cells just before). The idle
  hubs held 2.3-2.4 GiB right after the runs and were back to 190-286 MiB about
  15 minutes later, the same as before the roll.

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
- **Let age, not size, evict the hot lanes.** A binding `MaxBytes` or
  `MaxMsgsPer` evicts inside stream apply and takes the durable's lock per
  message (see 2026-10-11 above). The premium lane runs a 6 s age so its 400k
  per-subject cap binds only above ~66k msg/s; keep `age x peak rate` under
  both caps when either number moves.
- **In-flight cohorts** (`NATS_PUBLISH_INFLIGHT_COHORTS`, sesame runs 8). The
  fleet's in-flight window is the stream leader's apply backlog; 8 slots
  admit as much as 16 with 42% less commit latency.
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
3. Still present in 2.15.0, 2.15.1 and main: limit eviction (`enforceBytesLimit`,
   `enforcePerSubjectLimit`) removes one message at a time inside stream apply,
   and `storeUpdates` calls `decStreamPending` (consumer `o.mu`) per removal.
   With the durable's leader on the stream leader, apply blocked 13% on that
   lock and ingest 31% behind it, and one R3 stream admitted 40-46k msg/s
   instead of 62-67k; on a follower the same lock let that follower's apply
   fall 4 s behind. A batched removal callback (the `md < -1` branch already
   exists) would avoid both.

## Client distribution after a hub roll

BUS clients dial the `nats` Service only and ignore the pod IPs hubs advertise
in `connect_urls`, so every reconnect goes through Kubernetes readiness and the
node-local preference. After a rolling restart the last hub to come back still
holds few clients until clients reconnect for their own reasons; on 2026-10-11
the 227 BUS connections sat 0 / 5 / 222 across nats-0/1/2. Read the count per
hub with `/connz?limit=1024` on each pod. Lame duck (`nats-server --signal ldm`)
empties a hub: a maintenance tool, not a balancer.

## Reproducing

`deploy/k8s/bus-bench` is the rig: `-mode setup|publish|consume|seat-consumer|cleanup`.
It reports the latency split at the broker's stamp (`bus.Message.StoredAt`,
taken before the RAFT proposal), reconciles every sequence across pods, and
`seat-consumer -preferred stream-leader|not-stream-leader` recreates the
durable until its leader lands where the cell needs it, which is the one
variable that moved results by a third. Sample `/raftz?acc=<BUS>&group=<S-...>`
on every hub during a run: committed−applied per hub is where the time goes. Watch for two traps: an interrupted run leaves its publishers
alive inside the bench pods, and a bench consumer that gets OOM-killed silently
depresses the numbers.
