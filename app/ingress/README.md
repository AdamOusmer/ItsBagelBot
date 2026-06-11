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
3. Anything else: dropped.

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
| `NATS_CACHE_INVALIDATION_SUBJECT`   | Cache invalidation subject the ingress subscribes to.          | `bagel.cache.invalidate.broadcaster`          |
| `BROADCASTER_CACHE_TTL_SECONDS`     | Status cache TTL.                                              | `300`                                         |
| `LOG_LEVEL`                         | Logger level.                                                  | (inherited)                                   |

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

Production runs as a Mix release (`MIX_ENV=prod mix release`), one BEAM node per container, distribution bound to
the tailnet only.
