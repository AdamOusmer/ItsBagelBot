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
  lane_subject_stream:
    System.get_env("NATS_SUBJECT_LANE_STREAM", "twitch.ingress.event.stream"),
  invalidation_subject:
    System.get_env("NATS_CACHE_INVALIDATION_SUBJECT", "bagel.cache.invalidate.broadcaster"),
  # Request-reply subject answered by Ingress.AdminRpc with a cluster-wide
  # shard state snapshot. Consumed by the admin tool, never by user traffic.
  admin_subject: System.get_env("NATS_ADMIN_SUBJECT", "twitch.ingress.admin.shards.get"),
  # NATS RPC endpoint exposed by the Go service that owns broadcaster data.
  # The ingress never queries the database directly (data-and-state rules).
  broadcaster_status_subject:
    System.get_env("NATS_BROADCASTER_STATUS_SUBJECT", "bagel.rpc.broadcaster.status.get"),
  broadcaster_status_timeout_ms:
    String.to_integer(System.get_env("BROADCASTER_STATUS_TIMEOUT_MS", "2000")),
  broadcaster_cache_ttl_ms:
    String.to_integer(System.get_env("BROADCASTER_CACHE_TTL_SECONDS", "300")) * 1000

# Credentials are optional so local development can run against an open
# server; the production broker requires them and Gnat only sends them when
# the server asks (auth_required in the INFO handshake).
config :ingress,
  nats: %{
    host: System.get_env("NATS_HOST", "127.0.0.1"),
    port: String.to_integer(System.get_env("NATS_PORT", "4222")),
    username: System.get_env("NATS_USER"),
    password: System.get_env("NATS_PASSWORD")
  }

if level = System.get_env("LOG_LEVEL") do
  config :logger, level: String.to_existing_atom(level)
end

# New Relic. Without a license key the agent stays disabled and every API
# call is a no-op, so dev and test run unchanged.
config :new_relic_agent,
  app_name: System.get_env("NEW_RELIC_APP_NAME", "itsbagelbot-twitch-ingress"),
  license_key: System.get_env("NEW_RELIC_LICENSE_KEY")
