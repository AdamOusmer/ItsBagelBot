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
            application_name: System.get_env("BAGELBOT_K8S_APP_NAME", "ingress"),
            # Error-tolerant resolver: on a transient DNS failure it replays the
            # last good pod-IP set as a synthetic success, so a resolver error
            # never disconnects live peers (which would split the Horde cluster).
            # A genuine membership change is a successful lookup with a different
            # set and passes through untouched. See Ingress.ClusterResolver.
            resolver: &Ingress.ClusterResolver.resolve/1
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
  # Side-effect-free admin latency probe shared by every RPC-serving service.
  rpc_health_subject: System.get_env("NATS_RPC_HEALTH_SUBJECT", "bagel.rpc.health.ingress"),
  # Hard ceiling applied to both manual targets and the autoscaler estimate.
  # 123k NATS ceiling / 12k per-shard operating target = 11 useful shards.
  max_shards: String.to_integer(System.get_env("TWITCH_CONDUIT_MAX_SHARDS", "11")),
  # Capacity values are reported to the admin console and drive shard scaling.
  # The pod rating is the rounded-down production-shaped full-path benchmark;
  # the websocket rating is kept separate because adding a conduit shard adds
  # a socket read loop, not another pod's end-to-end processing capacity.
  capacity_pod_rated_eps:
    String.to_integer(System.get_env("INGRESS_CAPACITY_POD_RATED_EPS", "140000")),
  # Rounded down from the sustained three-node native-TLS direct-hub PubAck
  # result (123,834/s on 2026-07-13, with lane dedup off and leader-direct
  # dialing). This shared broker limit, not pod compute, currently sets
  # effective fleet throughput.
  capacity_nats_rated_eps:
    String.to_integer(System.get_env("INGRESS_CAPACITY_NATS_RATED_EPS", "123000")),
  # Rounded below the slowest production-node TLS 1.3 shard benchmark
  # (17,516/s); the 75% operating target therefore scales at 12,000/s.
  capacity_websocket_rated_eps:
    String.to_integer(System.get_env("INGRESS_CAPACITY_WEBSOCKET_RATED_EPS", "16000")),
  capacity_target_utilization_pct:
    String.to_integer(System.get_env("INGRESS_CAPACITY_TARGET_UTILIZATION_PCT", "75")),
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
  dispatcher_completion_batch_size:
    String.to_integer(System.get_env("INGRESS_DISPATCHER_COMPLETION_BATCH_SIZE", "4")),
  dispatcher_completion_flush_ms:
    String.to_integer(System.get_env("INGRESS_DISPATCHER_COMPLETION_FLUSH_MS", "25")),
  # Full trace/header creation is deliberately sparse at firehose rates. Set 1
  # only during a bounded diagnostic; set 0 to disable per-event traces.
  trace_sample_rate: String.to_integer(System.get_env("INGRESS_TRACE_SAMPLE_RATE", "1024")),
  squash_partitions:
    String.to_integer(
      System.get_env("INGRESS_SQUASH_PARTITIONS", Integer.to_string(System.schedulers_online()))
    ),
  # JetStream PubAck cohorts use one connection/collector per online BEAM
  # scheduler. Keeping these equal gives every event the same scheduler-local
  # path without idle connections or fallback routing.
  publish_connections:
    String.to_integer(
      System.get_env("INGRESS_PUBLISH_CONNECTIONS", Integer.to_string(System.schedulers_online()))
    ),
  # Per-connection queued + in-flight event bound; broker stalls remain bounded.
  publish_max_pending: String.to_integer(System.get_env("INGRESS_PUBLISH_MAX_PENDING", "16384")),
  publish_batch_size: String.to_integer(System.get_env("INGRESS_PUBLISH_BATCH_SIZE", "128")),
  publish_batch_wait_ms: String.to_integer(System.get_env("INGRESS_PUBLISH_BATCH_WAIT_MS", "1")),
  # Gnat coalesces up to eleven concurrent pub calls into one socket write.
  # Keep two write windows queued so the connection stays fed while the first
  # callers are being replied to, without creating one task/process per event.
  publish_send_concurrency:
    String.to_integer(System.get_env("INGRESS_PUBLISH_SEND_CONCURRENCY", "22")),
  # Cohort wire: "single" (per-event PubAck, default) or "atomic" (one ADR-050
  # batch commit ack per cohort; NATS 2.14). Anything unrecognized stays single.
  publish_wire:
    (case System.get_env("INGRESS_PUBLISH_WIRE", "single") do
       "atomic" -> :atomic
       _ -> :single
     end),
  publish_batch_inflight:
    String.to_integer(System.get_env("INGRESS_PUBLISH_BATCH_INFLIGHT", "4")),
  # Per-attempt PubAck wait and total attempt budget. Ingress is structurally
  # dedup-free: only definite negative PubAcks retry; ambiguous ack timeouts
  # drop instead of risking a double-store.
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

# Verify the NATS server TLS cert against the fleet CA now that NATS is out of the
# Linkerd mesh. NATS_CA_PEM is the trust-manager fleet-ca ConfigMap (PEM). Decode
# it to the DER list :ssl expects. Server-auth only — auth stays user/password. No
# CA (dev against a plaintext server) leaves the connection plaintext.
nats_cacerts =
  case System.get_env("NATS_CA_PEM") do
    pem when is_binary(pem) and pem != "" ->
      pem |> :public_key.pem_decode() |> Enum.map(fn {_type, der, _info} -> der end)

    _ ->
      nil
  end

nats_server = fn host, user, pass ->
  # Required for local-first HA: a missing node-qualified responder must return
  # immediately so Ingress.Rpc can safely fall back to the generic queue.
  base = %{host: host, port: nats_port, no_responders: true}

  base =
    if nats_cacerts do
      # SNI must match a cert SAN — the Service name (nats / nats-leaf).
      Map.merge(base, %{
        tls: true,
        ssl_opts: [
          verify: :verify_peer,
          cacerts: nats_cacerts,
          server_name_indication: String.to_charlist(host),
          depth: 3
        ]
      })
    else
      base
    end

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

# With the agent enabled, logs-in-context :forwarder mode replaces every log
# message with a JSON blob (message + metadata + entity/trace linking). The
# line must reach stdout as pure JSON for New Relic to parse it, so drop the
# plain-text time/level/metadata prefix; those fields already live inside the
# JSON. Without a license key the rewrite filter never installs, and the
# pretty dev format from config.exs stays.
if System.get_env("NEW_RELIC_LICENSE_KEY", "") != "" do
  config :logger, :default_formatter, format: "$message\n", metadata: []
end
