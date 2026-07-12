import Config

# All operational values arrive as environment variables and are read once at boot.
# See docs/src/content/docs/microservices/twitch-ingress.md for the contract.

split_csv = fn
  nil -> []
  "" -> []
  value -> value |> String.split(",") |> Enum.map(&String.trim/1) |> Enum.reject(&(&1 == ""))
end

# --- BEAM cluster auto-discovery -------------------------------------------
# Three modes, picked by environment:
#   * BAGELBOT_K8S_HEADLESS_SERVICE set: Kubernetes DNS strategy. Pod IPs are
#     discovered through the headless service; node names must be
#     <app_name>@<pod-ip> (RELEASE_DISTRIBUTION=name, RELEASE_NODE set from
#     the pod IP in the manifest). Multicast does not cross the CNI, so
#     Gossip cannot work in-cluster.
#   * BAGELBOT_CLUSTER_HOSTS set: EPMD strategy against fixed long names
#     (peers over the tailnet).
#   * neither: Gossip multicast for zero-config local development.
topologies =
  cond do
    headless = System.get_env("BAGELBOT_K8S_HEADLESS_SERVICE") ->
      [
        ingress: [
          strategy: Cluster.Strategy.Kubernetes.DNS,
          config: [
            service: headless,
            application_name: System.get_env("BAGELBOT_K8S_APP_NAME", "ingress")
          ]
        ]
      ]

    (hosts = split_csv.(System.get_env("BAGELBOT_CLUSTER_HOSTS"))) != [] ->
      [
        ingress: [
          strategy: Cluster.Strategy.Epmd,
          config: [hosts: Enum.map(hosts, &String.to_atom/1)]
        ]
      ]

    true ->
      [ingress: [strategy: Cluster.Strategy.Gossip]]
  end

config :ingress, cluster_topologies: topologies

config :ingress,
  twitch_client_id: System.get_env("TWITCH_CLIENT_ID"),
  twitch_client_secret: System.get_env("TWITCH_CLIENT_SECRET"),
  twitch_conduit_id: System.get_env("TWITCH_CONDUIT_ID"),
  conduit_shard_count: String.to_integer(System.get_env("TWITCH_CONDUIT_SHARD_COUNT", "2")),
  eventsub_url:
    System.get_env(
      "TWITCH_EVENTSUB_WSS_URL",
      "wss://eventsub.wss.twitch.tv/ws?keepalive_timeout_seconds=30"
    ),
  # Chatters whose messages always go to the premium lane, regardless of the
  # broadcaster's status. Provided by the secret store as a comma-separated
  # list of Twitch user IDs.
  special_user_ids: MapSet.new(split_csv.(System.get_env("TWITCH_SPECIAL_USER_IDS"))),
  lane_subject_premium:
    System.get_env("NATS_SUBJECT_LANE_PREMIUM", "twitch.ingress.event.premium"),
  lane_subject_standard:
    System.get_env("NATS_SUBJECT_LANE_STANDARD", "twitch.ingress.event.standard"),
  # Dedicated lane carrying only stream.online / stream.offline events.
  lane_subject_stream: System.get_env("NATS_SUBJECT_LANE_STREAM", "twitch.ingress.event.stream"),
  # Lane routing is a function of broadcaster status, so the ingress lane cache
  # only needs the "status" section of the scope-per-subject invalidation bus
  # (bagel.cache.invalidate.<scope>). Payload shape is unchanged ({broadcaster_id}).
  invalidation_subject:
    System.get_env("NATS_CACHE_INVALIDATION_SUBJECT", "bagel.cache.invalidate.status"),
  # Request-reply subject answered by Ingress.AdminRpc with a cluster-wide
  # shard state snapshot. Consumed by the admin tool, never by user traffic.
  admin_subject: System.get_env("NATS_ADMIN_SUBJECT", "twitch.ingress.admin.shards.get"),
  # Manual shard-count control: body {"count": N}, replies with full snapshot.
  scale_subject: System.get_env("NATS_SCALE_SUBJECT", "twitch.ingress.admin.shards.scale"),
  # Autoscaler toggle: body {"enabled": true|false}, replies with full snapshot.
  autoscale_subject:
    System.get_env("NATS_AUTOSCALE_SUBJECT", "twitch.ingress.admin.shards.autoscale"),
  # Live conduit id query: body {}, replies {"conduit_id": "<uuid>"} or {"error": "..."}.
  conduit_subject: System.get_env("NATS_CONDUIT_SUBJECT", "bagel.rpc.ingress.conduit.get"),
  # Hard ceiling applied to both manual targets and the autoscaler estimate.
  max_shards: String.to_integer(System.get_env("TWITCH_CONDUIT_MAX_SHARDS", "20")),
  # NATS RPC endpoint exposed by the Go service that owns broadcaster data.
  # The ingress never queries the database directly (data-and-state rules).
  broadcaster_status_subject:
    System.get_env("NATS_BROADCASTER_STATUS_SUBJECT", "bagel.rpc.broadcaster.status.get"),
  broadcaster_status_timeout_ms:
    String.to_integer(System.get_env("BROADCASTER_STATUS_TIMEOUT_MS", "2000")),
  broadcaster_cache_ttl_ms:
    String.to_integer(System.get_env("BROADCASTER_CACHE_TTL_SECONDS", "300")) * 1000,
  # Notification work is offloaded from shard websocket processes into this
  # bounded task dispatcher. Dropping here is better than letting socket
  # mailboxes grow until Twitch keepalives/reconnects are delayed.
  dispatcher_max_running:
    String.to_integer(System.get_env("INGRESS_DISPATCHER_MAX_RUNNING", "512")),
  dispatcher_max_queue:
    String.to_integer(System.get_env("INGRESS_DISPATCHER_MAX_QUEUE", "20000")),
  # Caps one broadcaster's share of the dispatcher budget above, so a hot
  # channel can't starve every other broadcaster sharing the pod.
  dispatcher_max_per_broadcaster:
    String.to_integer(System.get_env("INGRESS_DISPATCHER_MAX_PER_BROADCASTER", "2048")),
  dispatcher_broadcaster_sweep_ms:
    String.to_integer(System.get_env("INGRESS_DISPATCHER_BROADCASTER_SWEEP_MS", "60000")),
  # JetStream lane publishes block on the broker's PubAck (from a dispatcher
  # worker, so backpressure sheds at the dispatcher): per-attempt ack wait and
  # the total attempt budget. Retries are deduplicated broker-side by
  # Nats-Msg-Id, so they can never store an event twice.
  publish_ack_timeout_ms:
    String.to_integer(System.get_env("INGRESS_PUBLISH_ACK_TIMEOUT_MS", "2000")),
  publish_attempts: String.to_integer(System.get_env("INGRESS_PUBLISH_ATTEMPTS", "3"))

# Credentials are optional so local development can run against an open
# server; the production broker requires them and Gnat only sends them when
# the server asks (auth_required in the INFO handshake).
#
# Two planes on two accounts (per-account isolation), and they connect to
# DIFFERENT servers:
#   * :nats     — the twitch_ingress RPC account (NATS_RPC_USER/PASSWORD): admin
#     shard control, conduit RPC, broadcaster-status request, cache invalidation.
#     Stays on the node-local leaf — low-latency request/reply, and the per-service
#     RPC accounts are presented by the leaf.
#   * :nats_bus — the shared BUS account (NATS_USER/PASSWORD): the twitch.ingress.*
#     firehose captured by the JetStream streams. Connects DIRECT to the hub,
#     bypassing the leaf. The leaf runs no JetStream, so for the firehose it is
#     only a forwarding hop to the same hub streams; at the firehose rate that hop
#     is pure overhead, and the PubAcks come straight from the hub. Trade-off: a
#     hub roll re-pins this connection (Gnat reconnects); the RPC path is
#     untouched. In dev NATS_HUB_HOST is unset and falls back to the leaf host, so
#     both planes collapse onto one local server.
nats_leaf_host = System.get_env("NATS_LEAF_HOST") || System.get_env("NATS_HOST", "127.0.0.1")
nats_hub_host = System.get_env("NATS_HUB_HOST") || nats_leaf_host
nats_port = String.to_integer(System.get_env("NATS_PORT", "4222"))

nats_server = fn host, user, pass ->
  base = %{host: host, port: nats_port}

  if is_binary(user) and is_binary(pass) do
    Map.merge(base, %{username: user, password: pass})
  else
    base
  end
end

config :ingress,
  # RPC: leaf only.
  nats: [
    nats_server.(
      nats_leaf_host,
      System.get_env("NATS_RPC_USER") || System.get_env("NATS_USER"),
      System.get_env("NATS_RPC_PASSWORD") || System.get_env("NATS_PASSWORD")
    )
  ],
  # BUS firehose: hub only.
  nats_bus: [
    nats_server.(
      nats_hub_host,
      System.get_env("NATS_USER"),
      System.get_env("NATS_PASSWORD")
    )
  ]

if level = System.get_env("LOG_LEVEL") do
  config :logger, level: String.to_existing_atom(level)
end

# New Relic. Without a license key the agent stays disabled and every API
# call is a no-op, so dev and test run unchanged.
config :new_relic_agent,
  app_name: System.get_env("NEW_RELIC_APP_NAME", "itsbagelbot-twitch-ingress"),
  license_key: System.get_env("NEW_RELIC_LICENSE_KEY")
