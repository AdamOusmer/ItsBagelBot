<!-- Copyright (c) 2026 Adam Ousmer. All rights reserved.
     Proprietary. No license granted. See LICENSE.md. -->
# Twitch Ingress

Elixir/BEAM service that owns the Twitch EventSub **Conduit** and its WebSocket shards, filters incoming payloads,
and forwards normalized events to NATS. Design rationale: [ADR 0006](../../docs/src/content/docs/adr/0006-adoption-of-elixir-for-twitch-ingress.md),
full design: [microservices/twitch-ingress](../../docs/src/content/docs/microservices/twitch-ingress.md).

## What it does

- Forms a BEAM cluster via **libcluster** (EPMD strategy when `BAGELBOT_CLUSTER_HOSTS` is set, Gossip multicast
  auto-discovery otherwise). Shard ownership is distributed across nodes with **Horde**; when a node dies its shards
  re-home to survivors in seconds.
- A cluster-singleton `ConduitManager` reconciles the Conduit against Twitch (creates it, grows shard count, heals
  missing shard sessions) every 30s.
- One `ShardSession` GenServer per Conduit shard owns one WebSocket (raw Mint socket, no hidden lifecycle):
  - **Fresh connect:** waits for `session_welcome`, then binds the `session_id` to the shard via
    `PATCH /eventsub/conduits/shards`.
  - **No zombie connections:** a watchdog re-arms on every inbound frame; if Twitch's keepalive window (plus 5s
    grace) elapses silently, the socket is torn down and the shard reconnects with jittered exponential backoff
    (capped at 60s).
  - **`session_reconnect` is never skipped:** a second socket is opened to the provided `reconnect_url` while the
    old one keeps delivering; the old socket is closed only after the new one's `session_welcome`. If the handshake
    stalls past 30s, the shard falls back to a full fresh reconnect (which re-binds).

## Event routing

There are exactly three outbound lane subjects (`NATS_SUBJECT_LANE_*`). Every published event carries its EventSub
`type` in the payload; consumers filter on the payload, not the subject.

- `twitch.ingress.event.premium` / `twitch.ingress.event.standard`: all events, laned by broadcaster status.
- `twitch.ingress.event.stream`: **only** `stream.online` and `stream.offline`, regardless of broadcaster status
  (no cache lookup on this path).

For `channel.chat.message` notifications there are exactly three outcomes (`Ingress.Pipeline`):

1. Chatter is one of the **special user IDs** (`TWITCH_SPECIAL_USER_IDS`, from the secret store): publish to the
   **premium** lane, always, even when the broadcaster is on the free tier.
2. Message text starts with `!`: publish to the lane matching the **broadcaster's** status.
3. Plain chat: publish the first line immediately and fold identical lines into
   sender cohorts through the scheduler-sharded `Ingress.Squash.Pool`.

Broadcaster status is never read from MySQL directly (per the data-and-state ownership rules). It is fetched over
**NATS request-reply** from the owning Go service (`NATS_BROADCASTER_STATUS_SUBJECT`), through an in-process ETS
read-through cache (`Ingress.BroadcasterCache`, TTL `BROADCASTER_CACHE_TTL_SECONDS`). Cache entries are evicted by
invalidation messages on `NATS_CACHE_INVALIDATION_SUBJECT` (`{"broadcaster_id": "..."}`, `{"all": true}`, or a bare
ID). RPC failures degrade to the standard lane and are negative-cached for 5s.

All other EventSub types ride the same two lanes, routed by the event's broadcaster through the same cache. Events
without an extractable broadcaster (`broadcaster_user_id`, or `to_broadcaster_user_id` for inbound raids) default
to standard.

## Configuration

| Variable                            | Purpose                                                        | Default                                       |
|-------------------------------------|----------------------------------------------------------------|-----------------------------------------------|
| `BAGELBOT_CLUSTER_HOSTS`            | Comma-separated peer long-names (EPMD strategy). Empty: Gossip. | (empty, Gossip auto-discovery)               |
| `TWITCH_CLIENT_ID` / `TWITCH_CLIENT_SECRET` | App credentials for Helix.                             | (required)                                    |
| `TWITCH_CONDUIT_ID`                 | Conduit to own; empty: reuse first existing or create.         | (empty)                                       |
| `TWITCH_CONDUIT_SHARD_COUNT`        | Desired shard count.                                           | `2`                                           |
| `TWITCH_CONDUIT_MAX_SHARDS`         | Hard ceiling for manual and automatic shard counts.            | `11`                                          |
| `INGRESS_CAPACITY_POD_RATED_EPS`    | Measured cached-path capacity per live ingress pod.             | `140000`                                      |
| `INGRESS_CAPACITY_NATS_RATED_EPS`   | Live-cluster direct-hub PubAck capacity shared by the fleet.     | `123000`                                      |
| `INGRESS_CAPACITY_WEBSOCKET_RATED_EPS` | Rated read/decode capacity per conduit WebSocket.            | `16000`                                       |
| `INGRESS_CAPACITY_TARGET_UTILIZATION_PCT` | Autoscale and dashboard operating target.                  | `75`                                          |
| `INGRESS_PUBLISH_BATCH_SIZE`          | Scheduler-local JetStream publish cohort size.                 | `128`                                         |
| `INGRESS_PUBLISH_SEND_CONCURRENCY`    | Persistent Gnat send lanes per publisher connection.           | `22`                                          |
| `INGRESS_PUBLISH_BATCH_WAIT_MS`       | Maximum wait for a partially full publish cohort.               | `1`                                           |
| `INGRESS_PUBLISH_WIRE`                | Cohort wire: `atomic` (one commit PubAck per cohort, ADR-050) or `single` (per-event PubAck). An ambiguous outcome drops the whole cohort on `atomic` (up to `INGRESS_PUBLISH_BATCH_SIZE` = 128 events) against exactly 1 on `single` — see the blast-radius note below. Anything else warns on stderr and stays `atomic`. | `atomic` |
| `INGRESS_PUBLISH_BATCH_INFLIGHT`      | Unresolved atomic batches per shard. Sized by the FLEET budget, not by local latency: the broker caps open batches at 50 per stream and 3 replicas × 2 schedulers × 8 = 48. Cohorts over budget publish individually. | `8` |
| `INGRESS_PUBLISH_BATCH_HOLD_MS`       | How long a swept atomic batch keeps its local in-flight slot. Nothing on the wire cancels an open batch, so this must match the broker's batch inactivity timeout. | `10000`                  |
| `TWITCH_EVENTSUB_WSS_URL`           | EventSub WebSocket endpoint.                                   | `wss://eventsub.wss.twitch.tv/ws?...`         |
| `TWITCH_SPECIAL_USER_IDS`           | Comma-separated chatter IDs that always go premium.            | (empty)                                       |
| `NATS_HOST` / `NATS_PORT`           | NATS connection.                                               | `127.0.0.1` / `4222`                          |
| `NATS_SUBJECT_LANE_PREMIUM`         | Premium lane subject (all event types).                        | `twitch.ingress.event.premium`                |
| `NATS_SUBJECT_LANE_STANDARD`        | Standard lane subject (all event types).                       | `twitch.ingress.event.standard`               |
| `NATS_SUBJECT_LANE_STREAM`          | Dedicated lane for stream.online / stream.offline only.        | `twitch.ingress.event.stream`                 |
| `NEW_RELIC_LICENSE_KEY`             | Enables the New Relic agent; absent: agent disabled, no-op.    | (empty)                                       |
| `NEW_RELIC_APP_NAME`                | New Relic application name.                                    | `itsbagelbot-twitch-ingress`                  |
| `NATS_BROADCASTER_STATUS_SUBJECT`   | Request-reply subject for broadcaster status.                  | `bagel.rpc.broadcaster.status.get`            |
| `BROADCASTER_STATUS_TIMEOUT_MS`     | RPC timeout.                                                   | `2000`                                        |
| `NATS_CACHE_INVALIDATION_SUBJECT`   | Cache invalidation subject the ingress subscribes to.          | `bagel.cache.invalidate.status`               |
| `BROADCASTER_CACHE_TTL_SECONDS`     | Status cache TTL.                                              | `300`                                         |
| `INGRESS_SQUASH_PARTITIONS`         | Independent duplicate-cohort owners.                           | Online scheduler count                        |
| `INGRESS_DISPATCHER_MAX_RUNNING`    | Fixed direct-dispatch worker count.                             | `512`                                         |
| `INGRESS_DISPATCHER_MAX_QUEUE`      | Total bounded worker-mailbox allowance.                         | `20000`                                       |
| `INGRESS_DISPATCHER_COMPLETION_BATCH_SIZE` | Worker-local completion batch bound.                     | `4`                                           |
| `INGRESS_DISPATCHER_COMPLETION_FLUSH_MS` | Maximum wait for a partial completion batch.                | `25`                                          |
| `INGRESS_PUBLISH_CONNECTIONS`       | Independent NATS writers and PubAck collectors.                 | Online scheduler count                        |
| `INGRESS_PUBLISH_MAX_PENDING`       | In-flight PubAck window per NATS writer.                        | `16384`                                       |
| `LOG_LEVEL`                         | Logger level.                                                  | (inherited)                                   |

### Atomic-wire blast radius

Both wires are structurally dedup-free — no `Nats-Msg-Id` is ever attached — so
both follow the same rule: an *ambiguous* outcome (ack timeout, malformed
PubAck, a send error after part of the write is on the socket) drops rather
than replays, because a replay could double-store. Only a *definite* negative
PubAck, which proves the broker stored nothing, is re-driven.

What differs is how much one ambiguous outcome costs. `single` resolves one
event per PubAck, so it drops 1. `atomic` resolves the whole cohort with one
commit PubAck, so it drops up to `INGRESS_PUBLISH_BATCH_SIZE` = 128.

Nothing is lost silently: every dropped event is counted in
`Nats/PublishFailed`. The operator signal is that counter's **shape**, not its
existence — on `atomic` it moves in cohort-sized steps, on `single` one event
at a time. `Nats/PublishFailed` climbing in steps is the cue to set
`INGRESS_PUBLISH_WIRE=single` (a value edit in `deploy/k8s/twitch-ingress.yaml`,
no rebuild), which buys per-event granularity back at one RAFT quorum round trip
per event.

Two neighbouring counters mean different things and should not be alarmed
together: `Nats/PublishBatchBypassed` is the local in-flight window filling (a
commit-latency signal), while `Nats/PublishBatchFallback` is the broker
refusing or rejecting a batch and the cohort re-driving per message — if it
climbs with 10210/429s, the fleet has crossed the broker's 50-per-stream cap and
the replica or scheduler count no longer matches the arithmetic above.
`Nats/PublishBatchHeadersIgnored` is neither failure nor fallback: the broker
stored the cohort as ordinary publishes because it never read the
`Nats-Batch-*` headers, which only a pre-2.14 server does.

## Monitoring

New Relic via the official `new_relic_agent`. With `NEW_RELIC_LICENSE_KEY` unset the agent is disabled and every
instrumentation call is a no-op (dev and test run unchanged). Counters land under `Custom/Ingress/*`:
`Published/<lane>`, `Dropped`, `Shard/Reconnects`, `Shard/ZombieTimeouts`, `Shard/SessionReconnects`,
`Cache/Loads`, `Cache/LoadErrors`, `Nats/PublishDropped`. Shard lifecycle is queryable as the `IngressEvent`
custom event type (`ShardUp` / `ShardDown` with `shard_id`, `node`, `reason`). BEAM VM metrics (run queues, memory,
GC) are collected automatically by the agent.

## Running

```sh
mix deps.get
mix test
iex --sname ingress-a -S mix   # start a node; start a second one and Gossip will cluster them
```

To exercise the keepalive/reconnect flows locally, run the Twitch CLI mock EventSub server
(`twitch event websocket start-server`) and point `TWITCH_EVENTSUB_WSS_URL` at it.

Benchmarks are not kept in this repository. Load generators and capacity probes
belong in an operator's working copy for the duration of a measurement: when
they live in the tree they get built and published as container images on
unrelated commits, and they accumulate the broker grants and Falco exceptions
they need until those become permanent. If a hot-path change needs numbers,
write the harness locally and match the production VM's scheduler configuration
so the result transfers:

```sh
export ERL_FLAGS='+S 2:2 +SDcpu 2:2 +SDio 2 +sbwt short +sbwtdcpu none +sbwtdio none'
```

The capacity figures those measurements produced are not lost with the
harnesses: the per-pod event rating and the shard ceiling live as documented
constants in `config/runtime.exs`, and `test/capacity_test.exs` holds them.

The release builds on OTP 27 and uses its native `:json` codec for Twitch frame
decoding and NATS event encoding. Control-plane RPCs retain Jason where protocol
implementations such as `DateTime` are useful and are not on the firehose.

Production runs as a Mix release (`MIX_ENV=prod mix release`), one BEAM node per container, distribution bound to
the tailnet only.
