# Ingress: acknowledge completed effects

Date: 2026-11-16
Status: Proposed, A-lite. Owners: Sesame, ingress, messaging and affected data services.

## Recommendation

Use per-message AckExplicit on Sesame's premium and standard pull lanes. Acknowledge only after synchronous required effects succeed and every reporter generation the message joined has its DATA PubAck. Reporter flushes resolve completion tickets outside handler workers. Keep aggregation and existing DATA batch receipts; accept bounded replay and possible double counting across a crash. This does not promise exactly-once effects or recovery before broker acceptance.

References describe this worktree's `origin/main`, `f4ac5f5d5`, except the named local dependency below. Proposed firehose streams and consumers remain R3 memory. Use shared builders, propagate failures to the coarse failure path, and add neither fixed admission caps nor partial-failure cleanup loops (`AGENTS.md:35`, `AGENTS.md:37`, `AGENTS.md:38`, `AGENTS.md:39`).

## Constraints

Pull supports AckNone or AckAll, not AckExplicit (`pkg/bus/pull_consumer.go:72`). It records queue admission and advances a cumulative floor before effects complete (`pkg/bus/pull_consumer.go:300`, `pkg/bus/pull_consumer.go:370`). Changing only that policy cannot recover unpublished reporter windows.

Each of three hubs has JetStream `max_mem: 3GB` and a 5Gi pod limit (`deploy/messaging/nats-server.conf:53`, `deploy/messaging/nats.yaml:13`, `deploy/messaging/nats.yaml:111`). R3 places every memory replica on every hub. Existing memory stream budgets total 1,408 MiB, about 1.4 GiB (`pkg/bus/streams.go:34`, `pkg/bus/streams.go:77`, `pkg/bus/streams.go:96`, `pkg/bus/streams.go:108`, `pkg/bus/streams.go:120`, `pkg/bus/streams.go:131`). Reject a 120-second horizon at 20,000 events/s within this budget. MaxBytes bounds stored bytes, not RSS; delivery backlog also occupies hub memory outside pending counters (`deploy/messaging/README.md:148`).

DATA is a file-backed R3 handoff with finite retention (`pkg/bus/streams.go:56`). Earned credits and command uses commit batch receipts with SQL mutations (`app/db/loyalty/repository/earned_persist.go:62`, `app/db/commands/repository/uses.go:61`). Local dependency `fix/loyalty-counter-sql-receipts`, commit `b96a1769b`, admits counter receipts inside the mutation transaction (`app/db/loyalty/repository/counter_persist.go:41`, `app/db/loyalty/repository/counter_inbox.go:117`, at that ref). Its retention computes two hours, not 24 (`app/db/loyalty/repository/counter_retention.go:15`, at that ref). Reuse these immutable replayable batches; receipts must cover permitted DATA/DLQ replay.

Ingress sends no Nats-Msg-Id and drops ambiguous publication outcomes (`app/twitch/ingress/lib/ingress/nats/publisher/wire/single.ex:52`). A journal would require a persistent volume, a new operational unit absent today (`deploy/k8s/twitch-ingress.yaml:225`), to recover only locally received work. EventSub WebSockets cannot replay notifications missed while disconnected ([transport contract](https://dev.twitch.tv/docs/eventsub/handling-websocket-events/)); reconnect currently tears down/retries (`app/twitch/ingress/lib/ingress/shard_session.ex:721`). That limited benefit does not justify this phase's added ownership. Journal, publisher IDs, effects stream and per-event SQL receipts remain out of scope.

## Phases

### 1. Expose completion

Give each message tickets for all required reporter generations, including generations reached through reporter transfers. Seal each generation's existing batch IDs and payloads; resolve tickets only after every publication receives PubAck. Later success cannot cover earlier failure. Existing retries preserve batch identity (`app/twitch/sesame/engine/counter_publication.go:45`). Remove abandonment from required reporters; an abandoned batch cannot resolve successfully (`app/twitch/sesame/engine/loyalty_reporter.go:236`).

Shorten loyalty/use windows from five seconds to 250-500 ms (`app/twitch/sesame/engine/loyalty_reporter.go:21`, `app/twitch/sesame/engine/use_reporter.go:19`). Stats currently flush channels every 30 seconds into loyalty aggregation (`app/twitch/sesame/engine/bot_stats.go:19`, `app/twitch/sesame/engine/bot_stats.go:27`, `app/twitch/sesame/engine/bot_stats.go:312`). If required, flush around one second with the transfer inside the barrier. Analytics buffers ten seconds and drops failed Valkey flushes; inclusion needs an error-returning confirmed flush around one second, not a fictional DATA PubAck (`internal/analytics/recorder.go:24`, `internal/analytics/recorder.go:219`). Otherwise explicitly exclude stats/analytics as best effort, by owner decision.

After execution retain only ack reply subjects and compact ticket state, not wires/envelopes. Long payload windows are incompatible with Sesame's 512MiB GOMEMLIMIT (`deploy/k8s/sesame.yaml:135`). Pace pulls by execution/completion progress without a fixed admission ceiling.

Propagate currently logged command/handler failures into completion (`app/twitch/sesame/engine/pipeline_handlers.go:23`, `app/twitch/sesame/engine/pipeline_handlers.go:40`). Required sequencer/asynchronous tasks must join completion after handler return (`app/twitch/sesame/engine/streamseq.go:28`, `app/twitch/sesame/modules/live.go:107`). Audit detached mutations such as timer activity (`app/twitch/sesame/engine/timers_valkey.go:588`).

Move earn/use and counter completion claims after the confirmed effect or DATA handoff (`app/twitch/sesame/modules/loyalty.go:523`, `app/twitch/sesame/engine/dispatch.go:261`, `app/twitch/sesame/engine/dispatch.go:392`). Today's `Duplicate` claims through `Seen`, so a kill can suppress unpublished deltas (`app/twitch/sesame/engine/dedup.go:57`, `pkg/idempotency/valkey_store.go:42`). Preserve upstream-duplicate suppression using Twitch event/constituent IDs and completed-effect checks; in-progress claims cannot mean completion. The confirmation-to-claim crash gap remains.

### 2. Bound replay within memory

Split premium chat from `TWITCH_INGRESS` into a memory stream consumed only by Sesame. Standard already has its own stream; premium shares with stream/status traffic (`pkg/bus/streams.go:77`, `pkg/bus/streams.go:96`). Apply Interest retention, finite MaxAge initially 45-60 seconds, and DiscardOld to both hot chat streams. Remove MaxMsgsPer from dedicated premium: DiscardNew alone does not disable per-subject eviction (`pkg/bus/streams.go:90`, `pkg/bus/stream_config.go:33`).

Provision hot durables before publication. Disable inactivity deletion, currently inherited as five minutes (`pkg/bus/pull_consumer.go:56`, `pkg/bus/flow_consumer.go:29`). Interest without a matching consumer does not preserve work. Size MaxBytes within unchanged max_mem alongside legacy streams during cutover, the longer outgress window, consumer state and allocation headroom. The horizon depends on this budget and real-hub validation. Alert on oldest consumer lag approaching the effective horizon: age/byte eviction is silent (`deploy/messaging/README.md:154`). Longer stalls lose events.

Raise ordinary outgress MaxAge and Duplicates together to about 60 seconds, covering AckWait plus restart. Five-second MaxAge currently clamps dedup to five seconds (`pkg/bus/streams.go:38`, `pkg/bus/stream_config.go:18`), allowing duplicate replies after crash redelivery. Retain stable event/output IDs (`app/twitch/sesame/engine/pipeline_output.go:151`). Carry original event time into outgress and reject stale actions before execution, including batch items; the current contract lacks that timestamp (`internal/domain/outgress/message.go:8`). Owners choose freshness thresholds.

Reconcile explicit push stream/status lanes through existing `bindDurable`/`laneConsumerConfig` (`pkg/bus/bus.go:55`, `pkg/bus/bus.go:127`). Four-second AckWait, six deliveries and three-second NAK cannot fit the shared stream's six-second MaxAge (`pkg/bus/concurrent_subscriber.go:65`, `pkg/bus/bus.go:27`, `pkg/bus/bus.go:275`, `pkg/bus/streams.go:86`). Align retention/retry timing so the intended retry/DLQ sequence can complete.

### 3. Activate explicit acknowledgements

Require hot pull AckExplicit, AckWait initially about ten seconds, MaxDeliver=-1, delayed NAK for transient failure, MaxAckPending=-1 and inactivity deletion off. Change the positive-only pending parser (`pkg/bus/pull_consumer.go:98`). Remove AckAll receipt floors and retire one-hop republish retry for these lanes (`pkg/bus/pull_consumer.go:370`, `pkg/bus/pull_consumer.go:416`). Ack only when phase-1 completion resolves; a kill leaves unfinished work available within retention.

Quarantine malformed/poison input through existing DATA_DLQ, requiring PubAck before source acknowledgement. Extend routing/permissions through `pkg/bus`; the existing helper selects only DATA and terminates even after DLQ publication fails (`pkg/bus/deadletter.go:35`, `pkg/bus/deadletter.go:39`, `pkg/bus/deadletter.go:61`). Unlimited transient retries must not strand poison indefinitely.

Activate after compatible consumers/dependencies exist. Update split-stream lane detection (`pkg/bus/lane_mode.go:12`). Cut over without dual-counting; rollback retains compatible explicit durables, never recreating them with DeliverNew or restoring AckNone.

## Rejected options

- **AckAll floors:** another pod's later completion can acknowledge earlier unfinished deliveries. Local floors cannot coordinate the shared durable.
- **Option C, per-event effects stream and SQL receipts:** roughly doubles broker traffic against the admission bottleneck (`deploy/messaging/README.md:148`) and projects 1.73 billion receipt rows/day at one per event at 20,000/s, before additional effects. Reuse batch receipts.
- **Interest/MaxAge=0/DiscardNew plus journal:** a stalled outgress/projector durable can fill the shared stream and reject newest chat fleet-wide, with no poison age exit (`deploy/messaging/accounts.yaml:266`, `deploy/messaging/accounts.yaml:308`). This global admission cap still permits per-subject eviction; adding a volume-backed journal does not fix it.

## Release gates and residual risks

On real R3 hubs, kill Sesame at 20,000 events/s. Measure hub CPU/Raft load from explicit acks, peak hub RSS, redelivery latency, missing earned/use/command effects and duplicate replies. Inject failures before DATA PubAck, between PubAck/source ack, during reporter transfer and during DLQ publication. Verify actual consumer configuration, short windows, retained payload memory, stale-action rejection and push-lane recovery. These are gates, not capacity claims; tools/results stay outside the repository.

Residual risks: confirmed batches can regroup and double count in the PubAck-to-ack gap; excluded stats remain approximate; stalls beyond the horizon evict events; ingress crashes lose publisher queues (`app/twitch/ingress/lib/ingress/nats/publisher.ex:181`); replies arrive about AckWait late after a kill; losing all memory replicas loses retained work. Cooldowns can suppress replayed replies (`app/twitch/sesame/engine/dispatch.go:108`, `app/twitch/sesame/engine/dispatch.go:455`); timers use processing time (`app/twitch/sesame/engine/timers_valkey.go:608`). A-lite does not reconstruct those decisions or make external actions exactly once.

## Owner decisions

- Required effects; stats/analytics inside completion or best effort.
- Recovery horizon, memory allocation, lag alerts, retry delay and outgress freshness thresholds, subject to release gates.
- Premium-stream and extended-DLQ ownership/permissions. Any new operational unit needs an explicit owner decision.
- Counter SQL receipt dependency and proposed 24-hour retention versus current two hours, with a replay boundary covering DATA/DLQ.
- Acceptance of residual duplicate/loss windows and compatible cutover/rollback behavior.
