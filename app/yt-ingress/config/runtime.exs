# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

import Config

# All operational values arrive as environment variables and are read once at boot.
# See docs/src/content/docs/microservices/youtube-ingress.md for the contract.

split_csv = fn
  nil -> []
  "" -> []
  value -> value |> String.split(",") |> Enum.map(&String.trim/1) |> Enum.reject(&(&1 == ""))
end

# --- BEAM cluster auto-discovery -------------------------------------------
# Same three modes as the Twitch ingress:
#   * BAGELBOT_K8S_HEADLESS_SERVICE set: Kubernetes DNS strategy against the
#     headless service; node names must be <app_name>@<pod-ip>.
#   * BAGELBOT_CLUSTER_HOSTS set: EPMD strategy against fixed long names.
#   * neither: Gossip multicast for zero-config local development.
topologies =
  cond do
    headless = System.get_env("BAGELBOT_K8S_HEADLESS_SERVICE") ->
      [
        yt_ingress: [
          strategy: Cluster.Strategy.Kubernetes.DNS,
          config: [
            service: headless,
            application_name: System.get_env("BAGELBOT_K8S_APP_NAME", "yt-ingress"),
            # Error-tolerant resolver: a transient DNS failure replays the last
            # good pod-IP set instead of disconnecting live peers. See
            # YtIngress.ClusterResolver.
            resolver: &YtIngress.ClusterResolver.resolve/1
          ]
        ]
      ]

    (hosts = split_csv.(System.get_env("BAGELBOT_CLUSTER_HOSTS"))) != [] ->
      [
        yt_ingress: [
          strategy: Cluster.Strategy.Epmd,
          config: [hosts: Enum.map(hosts, &String.to_atom/1)]
        ]
      ]

    true ->
      [yt_ingress: [strategy: Cluster.Strategy.Gossip]]
  end

config :yt_ingress, cluster_topologies: topologies

config :yt_ingress,
  # googleapis gRPC endpoint carrying liveChatMessages.streamList.
  youtube_grpc_host: System.get_env("YOUTUBE_GRPC_HOST", "youtube.googleapis.com"),
  youtube_grpc_port: String.to_integer(System.get_env("YOUTUBE_GRPC_PORT", "443")),
  # Static credential fallbacks so local development can stream a chat without
  # the users service behind them. Production resolves per-channel OAuth access
  # tokens over NATS RPC from the service that owns Google credentials.
  youtube_api_key: System.get_env("YOUTUBE_API_KEY"),
  # Channels this ingress watches, as comma-separated YouTube channel IDs. The
  # long-term source of truth is the users service over NATS RPC; until that
  # endpoint ships, the watch list is provisioned through this variable.
  channel_ids: MapSet.new(split_csv.(System.get_env("YOUTUBE_CHANNEL_IDS"))),
  # Dev escape hatch: pin live chat IDs directly and skip broadcast discovery.
  pinned_live_chat_ids: split_csv.(System.get_env("YOUTUBE_LIVE_CHAT_IDS")),
  # Silence budget before a chat session presumes its stream dead and
  # reconnects (resuming from the last page token). Must sit well above any
  # genuine lull between chat messages.
  chat_idle_timeout_seconds:
    String.to_integer(System.get_env("YT_CHAT_IDLE_TIMEOUT_SECONDS", "300")),
  # How often the watcher re-checks each watched channel for an active
  # broadcast. liveBroadcasts.list costs 1 quota unit, so 30s polling is
  # 2,880 units/channel/day inside the default 10k/day project budget.
  watcher_poll_seconds: String.to_integer(System.get_env("YOUTUBE_WATCH_POLL_SECONDS", "30")),
  youtube_data_api_url:
    System.get_env("YOUTUBE_DATA_API_URL", "https://www.googleapis.com/youtube/v3"),
  # Author channel IDs whose messages always route to the premium lane,
  # regardless of the broadcaster's status. Provided by the secret store.
  special_user_ids: MapSet.new(split_csv.(System.get_env("YOUTUBE_SPECIAL_USER_IDS"))),
  lane_subject_premium:
    System.get_env("NATS_SUBJECT_LANE_PREMIUM", "youtube.ingress.event.premium"),
  lane_subject_standard:
    System.get_env("NATS_SUBJECT_LANE_STANDARD", "youtube.ingress.event.standard"),
  # Dedicated lane carrying only broadcast lifecycle events (the streamList
  # analogue of stream.online / stream.offline), regardless of broadcaster status.
  lane_subject_stream: System.get_env("NATS_SUBJECT_LANE_STREAM", "youtube.ingress.event.stream"),
  # Lane routing is a function of broadcaster status, so the lane cache only
  # needs the "status" section of the scope-per-subject invalidation bus
  # (bagel.cache.invalidate.<scope>). Same payload shape as the Twitch ingress
  # ({broadcaster_id}).
  invalidation_subject:
    System.get_env("NATS_CACHE_INVALIDATION_SUBJECT", "bagel.cache.invalidate.status"),
  # Request-reply subject answered by YtIngress.AdminRpc with a cluster-wide
  # snapshot of watchers and open chat streams. Consumed by the admin tool.
  admin_subject: System.get_env("NATS_ADMIN_SUBJECT", "youtube.ingress.admin.chats.get"),
  # Side-effect-free admin latency probe shared by every RPC-serving service.
  rpc_health_subject: System.get_env("NATS_RPC_HEALTH_SUBJECT", "bagel.rpc.health.yt-ingress"),
  # Hard ceiling applied to chat text before it is published. A well-formed
  # YouTube live chat line is far shorter; the ceiling is generous abuse cover.
  max_chat_text_bytes: String.to_integer(System.get_env("YT_MAX_CHAT_TEXT_BYTES", "4096")),
  # One in N events receives transaction and trace headers. Zero disables
  # per-event tracing; one is reserved for controlled diagnostics.
  trace_sample_rate: String.to_integer(System.get_env("YT_INGRESS_TRACE_SAMPLE_RATE", "1024")),
  # NATS RPC endpoint exposed by the Go service that owns broadcaster data.
  # The ingress never queries the database directly (data-and-state rules).
  broadcaster_status_subject:
    System.get_env("NATS_BROADCASTER_STATUS_SUBJECT", "bagel.rpc.broadcaster.status.get"),
  broadcaster_status_timeout_ms:
    String.to_integer(System.get_env("BROADCASTER_STATUS_TIMEOUT_MS", "2000")),
  broadcaster_cache_ttl_ms:
    String.to_integer(System.get_env("BROADCASTER_CACHE_TTL_SECONDS", "300")) * 1000,
  # NATS RPC endpoint exposing short-lived Google OAuth access tokens for a
  # linked YouTube channel: {"channel_id": "..."} ->
  # {"access_token": "...", "expires_at": <unix seconds>}. Owned by the users
  # service once its YouTube side ships; until then the static API-key path
  # carries development.
  token_subject: System.get_env("NATS_YT_TOKEN_SUBJECT", "bagel.rpc.youtube.token.get"),
  token_timeout_ms: String.to_integer(System.get_env("YT_TOKEN_TIMEOUT_MS", "2000")),
  # A cached access token is refreshed this many seconds ahead of its expiry.
  token_refresh_margin_seconds:
    String.to_integer(System.get_env("YT_TOKEN_REFRESH_MARGIN_SECONDS", "60"))

# --- NATS planes ------------------------------------------------------------
# Two planes on two accounts, identical to the Twitch ingress wiring:
#   * :nats     — the yt_ingress RPC account (NATS_RPC_USER/PASSWORD): admin
#     RPC, broadcaster-status request, cache invalidation. Stays on the
#     node-local leaf.
#   * :nats_bus — the shared BUS account (NATS_USER/PASSWORD): the
#     youtube.ingress.* firehose captured by the JetStream streams. Connects
#     DIRECT to the hub, bypassing the leaf. In dev NATS_HUB_HOST is unset and
#     both planes collapse onto one local server.
nats_leaf_host = System.get_env("NATS_LEAF_HOST") || System.get_env("NATS_HOST", "127.0.0.1")
nats_hub_host = System.get_env("NATS_HUB_HOST") || nats_leaf_host
nats_port = String.to_integer(System.get_env("NATS_PORT", "4222"))

# Verify the NATS server TLS cert against the fleet CA now that NATS is out of
# the Linkerd mesh. NATS_CA_PEM is the trust-manager fleet-ca ConfigMap (PEM).
# No CA (dev against a plaintext server) leaves the connection plaintext.
nats_cacerts =
  case System.get_env("NATS_CA_PEM") do
    pem when is_binary(pem) and pem != "" ->
      pem |> :public_key.pem_decode() |> Enum.map(fn {_type, der, _info} -> der end)

    _ ->
      nil
  end

nats_server = fn host, user, pass ->
  base = %{host: host, port: nats_port, no_responders: true}

  base =
    if nats_cacerts do
      # SNI must match a cert SAN — the Service name (nats / nats-leaf).
      ssl_opts = [
        verify: :verify_peer,
        cacerts: nats_cacerts,
        server_name_indication: String.to_charlist(host),
        depth: 3
      ]

      # mTLS: present the fleet client cert when the pair is set. Both or
      # neither — a half-set pair fails the boot rather than silently
      # downgrading to server-auth only. File paths (not inlined PEM) so the
      # BEAM ssl pem cache re-reads a cert-manager renewal without a restart.
      ssl_opts =
        case {System.get_env("NATS_CLIENT_CERT_FILE", ""),
              System.get_env("NATS_CLIENT_KEY_FILE", "")} do
          {"", ""} ->
            ssl_opts

          {cert, key} when cert != "" and key != "" ->
            ssl_opts ++ [certfile: cert, keyfile: key]

          _ ->
            raise "NATS_CLIENT_CERT_FILE and NATS_CLIENT_KEY_FILE must both be set or both empty"
        end

      Map.merge(base, %{tls: true, ssl_opts: ssl_opts})
    else
      base
    end

  if is_binary(user) and is_binary(pass) do
    Map.merge(base, %{username: user, password: pass})
  else
    base
  end
end

config :yt_ingress,
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
  app_name: System.get_env("NEW_RELIC_APP_NAME", "itsbagelbot-youtube-ingress"),
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
