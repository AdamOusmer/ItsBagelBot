# Twitch ingress context

Agent guide for the Elixir EventSub receiver. Read [shared language](../../../CONTEXT.md)
and the [app map](../../../CONTEXT-MAP.md) for neighboring owners.
This describes the current merged Elixir routing and trial observation code.

## Purpose and boundary

Ingress owns the Twitch EventSub Conduit and its WebSocket shard sessions.
It receives, normalizes, routes and publishes events; it does not run commands
or impose moderation consequences. Sesame owns command behavior; Twitch outgress
owns authenticated Twitch execution and EventSub subscription enrollment.
Ingress may call Helix to manage its Conduit and bind shard sessions.
It obtains broadcaster status through RPC, never by reading MySQL.

## Local nomenclature

- **Conduit:** Twitch's delivery resource distributing subscriptions over shards.
- **Shard:** one Conduit slot; not a Kubernetes pod or a duplicate-chat owner.
- **Shard session:** supervised owner of the socket currently bound to a shard.
- **Singleton:** cluster-wide manager/scaler ownership registered through Horde.
- **Lane:** premium, standard or stream transport priority, carried in payloads.
- **Special user:** configured chatter ID whose chat always gets premium priority;
  this does not grant the broadcaster Premium access.
- **Squash/cohort:** identical plain-chat text folded with individual senders.
- **Trial:** observed unregistered channel; trusted receiver metadata marks its
  events so downstream services cannot act for it. See the root glossary.
- **PubAck:** JetStream storage verdict, distinct from local publish admission.

## Start with these files

| Change | Read first |
| --- | --- |
| Boot/cluster/service ownership | [application.ex](lib/ingress/application.ex), [bootstrapper.ex](lib/ingress/bootstrapper.ex), [singleton.ex](lib/ingress/singleton.ex) |
| Socket watchdog, reconnect, handoff | [shard_session.ex](lib/ingress/shard_session.ex), [ws.ex](lib/ingress/ws.ex), [drain.ex](lib/ingress/drain.ex) |
| Conduit reconciliation and placement | [conduit_manager.ex](lib/ingress/conduit_manager.ex), [shard_distribution.ex](lib/ingress/shard_distribution.ex) |
| Event shape and priority | [pipeline.ex](lib/ingress/pipeline.ex): `handle_event/2`, `route/2`, `decide/3`, `emote_spans/1` |
| Duplicate chat handling | [squash.ex](lib/ingress/squash.ex), [squash/pool.ex](lib/ingress/squash/pool.ex) |
| Bounded delivery and broker verdicts | [dispatcher.ex](lib/ingress/dispatcher.ex), [nats.ex](lib/ingress/nats.ex), [publisher.ex](lib/ingress/nats/publisher.ex), [wire.ex](lib/ingress/nats/publisher/wire.ex) |
| Status lookup/invalidation | [broadcaster_cache.ex](lib/ingress/broadcaster_cache.ex), [cache_invalidator.ex](lib/ingress/cache_invalidator.ex) |
| Trials and registration eviction | [trial_receiver.ex](lib/ingress/trial_receiver.ex), [trials.ex](lib/ingress/trials.ex), [trial_user_changed.ex](lib/ingress/trial_user_changed.ex) |
| Capacity and scaling | [capacity.ex](lib/ingress/capacity.ex), [shard_scaler.ex](lib/ingress/shard_scaler.ex), [shard_scaler/policy.ex](lib/ingress/shard_scaler/policy.ex) |
| Effective configuration | [runtime.exs](config/runtime.exs), [config.ex](lib/ingress/config.ex), `lib/ingress/config/` |
| HTTP/RPC health and telemetry | [status_plug.ex](lib/ingress/status_plug.ex), [health.ex](lib/ingress/health.ex), [metrics.ex](lib/ingress/metrics.ex), [trace.ex](lib/ingress/trace.ex) |

## Routing and shared contracts

1. Shard sessions decode notifications and hand bounded work to the dispatcher.
2. `Pipeline.route/2` checks chat size, extracts broadcaster and chooses a lane.
3. Special chatter IDs precede command detection. Commands tolerate leading
   whitespace and bypass squash; repeated commands remain separate invocations.
4. Plain chat publishes its first occurrence immediately; subsequent equal lines
   are folded with sender identity/timing. Do not discard those sender records.
5. Stream online/offline events are dual-published to the dedicated stream lane
   and the broadcaster's ordinary lane. Banned channels lose the ordinary copy.
6. Unknown broadcaster extraction falls back to standard. Cache lookup failures
   degrade to standard and have a short negative-cache window.

Legacy subjects are `twitch.ingress.event.{premium,standard,stream}`.
Chat has a flattened envelope; non-chat events retain nested `event` data.
Preserve `type`, `lane`, `msg_id`, event identity, receive time and shard metadata.
`chat_message_id` identifies the actual Twitch chat line, while notification IDs
identify transport events. Emote offsets count Unicode codepoints, end-exclusive.
There is no Rust AutoMod v2 routing flag or `chat_input` adapter in this checkout.
Trial provenance is derived from receiver metadata only; trial reception and
Conduit membership have their own lifecycle and generation handling.

The RPC plane and BUS firehose use separate NATS identities/connections.
Status lookup defaults to `bagel.rpc.broadcaster.status.get`; invalidations to
`bagel.cache.invalidate.status`. Admin/scale/trial/Conduit responders are wired
in `application.ex`; consult `rpc.ex` before changing subject aliases.

## Invariants and pitfalls

- Horde shard redistribution is passive; deterministic placement and graceful
  make-before-break drain handle scaling/rollouts. Do not add a competing mover.
- Session reconnect opens the replacement before closing the delivering socket;
  keepalive timeout and failed reconnect must still lead to a fresh rebind.
- Queues, dispatch mailboxes and in-flight publishes are explicitly bounded.
- Fleet publication carries no broker dedup ID. Ambiguous PubAck/send outcomes
  drop rather than replay; only definite broker negatives justify re-driving.
- Atomic publish wire resolves a whole cohort; one ambiguous verdict can lose up
  to the batch bound. Single wire reduces that blast radius to one event.
- Root vocabulary distinguishes Twitch VIP badges from ItsBagelBot VIP accounts.
- The README has historical routing prose; `pipeline.ex` is authoritative for
  dual-published stream events.

## Local verification

Run from `app/twitch/ingress` with the Elixir/OTP versions in [Containerfile](Containerfile).
The application disables service startup in tests; inspect [test_helper.exs](test/test_helper.exs).

```sh
mix deps.get
mix test
mix test test/ingress_test.exs test/shard_session_test.exs
mix test test/nats_publisher_atomic_test.exs test/nats_publisher_wire_test.exs
```

For connected local runs, use the configuration contract in [README.md](README.md):
`iex --sname ingress-a -S mix` starts a live node and needs service credentials.
Do not run a second owner against production merely to exercise a change.
