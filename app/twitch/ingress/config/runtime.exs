# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

import Config

split_csv = fn
  nil -> []
  "" -> []
  value -> value |> String.split(",") |> Enum.map(&String.trim/1) |> Enum.reject(&(&1 == ""))
end

topologies =
  cond do
    headless = System.get_env("BAGELBOT_K8S_HEADLESS_SERVICE") ->
      [
        ingress: [
          strategy: Cluster.Strategy.Kubernetes.DNS,
          config: [
            service: headless,
            application_name: System.get_env("BAGELBOT_K8S_APP_NAME", "ingress"),
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

publish_wire =
  case System.get_env("INGRESS_PUBLISH_WIRE", "atomic") do
    "atomic" ->
      :atomic

    "single" ->
      :single

    unknown ->
      IO.warn(
        "INGRESS_PUBLISH_WIRE=#{inspect(unknown)} is not a known cohort wire; " <>
          "expected \"atomic\" or \"single\", using \"atomic\"",
        []
      )

      :atomic
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
  special_user_ids: MapSet.new(split_csv.(System.get_env("TWITCH_SPECIAL_USER_IDS"))),
  lane_subject_premium:
    System.get_env("NATS_SUBJECT_LANE_PREMIUM", "twitch.ingress.event.premium"),
  lane_subject_standard:
    System.get_env("NATS_SUBJECT_LANE_STANDARD", "twitch.ingress.event.standard"),
  lane_subject_stream: System.get_env("NATS_SUBJECT_LANE_STREAM", "twitch.ingress.event.stream"),
  invalidation_subject:
    System.get_env("NATS_CACHE_INVALIDATION_SUBJECT", "bagel.cache.invalidate.status"),
  admin_subject: System.get_env("NATS_ADMIN_SUBJECT", "twitch.ingress.admin.shards.get"),
  scale_subject: System.get_env("NATS_SCALE_SUBJECT", "twitch.ingress.admin.shards.scale"),
  autoscale_subject:
    System.get_env("NATS_AUTOSCALE_SUBJECT", "twitch.ingress.admin.shards.autoscale"),
  conduit_subject: System.get_env("NATS_CONDUIT_SUBJECT", "bagel.rpc.ingress.conduit.get"),
  rpc_health_subject: System.get_env("NATS_RPC_HEALTH_SUBJECT", "bagel.rpc.health.ingress"),
  max_shards: String.to_integer(System.get_env("TWITCH_CONDUIT_MAX_SHARDS", "11")),
  capacity_pod_rated_eps:
    String.to_integer(System.get_env("INGRESS_CAPACITY_POD_RATED_EPS", "140000")),
  capacity_nats_rated_eps:
    String.to_integer(System.get_env("INGRESS_CAPACITY_NATS_RATED_EPS", "123000")),
  capacity_websocket_rated_eps:
    String.to_integer(System.get_env("INGRESS_CAPACITY_WEBSOCKET_RATED_EPS", "16000")),
  capacity_target_utilization_pct:
    String.to_integer(System.get_env("INGRESS_CAPACITY_TARGET_UTILIZATION_PCT", "75")),
  broadcaster_status_subject:
    System.get_env("NATS_BROADCASTER_STATUS_SUBJECT", "bagel.rpc.broadcaster.status.get"),
  broadcaster_status_timeout_ms:
    String.to_integer(System.get_env("BROADCASTER_STATUS_TIMEOUT_MS", "2000")),
  broadcaster_cache_ttl_ms:
    String.to_integer(System.get_env("BROADCASTER_CACHE_TTL_SECONDS", "300")) * 1000,
  dispatcher_max_running:
    String.to_integer(System.get_env("INGRESS_DISPATCHER_MAX_RUNNING", "512")),
  dispatcher_max_queue:
    String.to_integer(System.get_env("INGRESS_DISPATCHER_MAX_QUEUE", "20000")),
  dispatcher_max_per_broadcaster:
    String.to_integer(System.get_env("INGRESS_DISPATCHER_MAX_PER_BROADCASTER", "2048")),
  dispatcher_broadcaster_sweep_ms:
    String.to_integer(System.get_env("INGRESS_DISPATCHER_BROADCASTER_SWEEP_MS", "60000")),
  dispatcher_completion_batch_size:
    String.to_integer(System.get_env("INGRESS_DISPATCHER_COMPLETION_BATCH_SIZE", "4")),
  dispatcher_completion_flush_ms:
    String.to_integer(System.get_env("INGRESS_DISPATCHER_COMPLETION_FLUSH_MS", "25")),
  trace_sample_rate: String.to_integer(System.get_env("INGRESS_TRACE_SAMPLE_RATE", "1024")),
  squash_partitions:
    String.to_integer(
      System.get_env("INGRESS_SQUASH_PARTITIONS", Integer.to_string(System.schedulers_online()))
    ),
  publish_connections:
    String.to_integer(
      System.get_env("INGRESS_PUBLISH_CONNECTIONS", Integer.to_string(System.schedulers_online()))
    ),
  publish_max_pending: String.to_integer(System.get_env("INGRESS_PUBLISH_MAX_PENDING", "16384")),
  publish_batch_size: String.to_integer(System.get_env("INGRESS_PUBLISH_BATCH_SIZE", "128")),
  publish_batch_wait_ms: String.to_integer(System.get_env("INGRESS_PUBLISH_BATCH_WAIT_MS", "1")),
  publish_send_concurrency:
    String.to_integer(System.get_env("INGRESS_PUBLISH_SEND_CONCURRENCY", "22")),
  publish_wire: publish_wire,
  # replicas x schedulers x this must stay under the broker's max_inflight_per_stream.
  publish_batch_inflight:
    String.to_integer(System.get_env("INGRESS_PUBLISH_BATCH_INFLIGHT", "8")),
  # Must equal the broker's batch inactivity timeout (jetstream.limits.batch.timeout).
  publish_batch_hold_ms:
    String.to_integer(System.get_env("INGRESS_PUBLISH_BATCH_HOLD_MS", "10000")),
  publish_ack_timeout_ms:
    String.to_integer(System.get_env("INGRESS_PUBLISH_ACK_TIMEOUT_MS", "2000")),
  publish_attempts: String.to_integer(System.get_env("INGRESS_PUBLISH_ATTEMPTS", "3"))

nats_leaf_host = System.get_env("NATS_LEAF_HOST") || System.get_env("NATS_HOST", "127.0.0.1")
nats_hub_host = System.get_env("NATS_HUB_HOST") || nats_leaf_host
nats_port = String.to_integer(System.get_env("NATS_PORT", "4222"))

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
      ssl_opts = [
        verify: :verify_peer,
        cacerts: nats_cacerts,
        server_name_indication: String.to_charlist(host),
        depth: 3
      ]

      # File paths, not contents: ssl re-reads cert-manager renewals.
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

config :ingress,
  nats: [
    nats_server.(
      nats_leaf_host,
      System.get_env("NATS_RPC_USER") || System.get_env("NATS_USER"),
      System.get_env("NATS_RPC_PASSWORD") || System.get_env("NATS_PASSWORD")
    )
  ],
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

config :new_relic_agent,
  app_name: System.get_env("NEW_RELIC_APP_NAME", "itsbagelbot-twitch-ingress"),
  license_key: System.get_env("NEW_RELIC_LICENSE_KEY")

if System.get_env("NEW_RELIC_LICENSE_KEY", "") != "" do
  config :logger, :default_formatter, format: "$message\n", metadata: []
end
