# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.Application do
  @moduledoc """
  Top-level supervisor for the YouTube ingress.

  Tree (`:one_for_one`, mirroring the Twitch ingress):

    * `Cluster.Supervisor` (libcluster) - BEAM node auto-discovery.
    * `YtIngress.Registry` (Horde) - cluster-wide
      `{:chat, channel_id} -> pid` registry.
    * `YtIngress.ChatSupervisor` (Horde) - spawns chat sessions, re-assigns
      them to surviving nodes when a node leaves.
    * `Gnat.ConnectionSupervisor` - RPC-plane NATS connection (yt_ingress
      account), registered as `:gnat`.
    * `Gnat.ConnectionSupervisor` - BUS-plane NATS connection (shared BUS
      account), registered as `:gnat_bus`; carries the youtube.ingress.*
      firehose.
    * `YtIngress.Metrics` - scheduler-friendly ETS counter aggregation; New
      Relic is called once per counter per flush rather than once per event.
    * `Task.Supervisor`s for the broadcaster cache, token fetches and the
      per-session stream readers (all off-the-hot-path work).
    * `YtIngress.BroadcasterCache` - in-process ETS cache over the
      broadcaster status NATS RPC (the ingress never reads the database
      directly).
    * `YtIngress.TokenSource` - per-channel Google credential resolution over
      NATS RPC, cached until expiry minus margin.
    * `GRPC.Client.Supervisor` - gRPC client connection machinery backing
      every session's streamList channel.
    * `Gnat.ConsumerSupervisor` (invalidation) - subscription to cache
      invalidation subject.
    * `Gnat.ConsumerSupervisor` (admin) - request-reply snapshot endpoint for
      `YtIngress.AdminRpc`.
    * `Gnat.ConsumerSupervisor` (health) - side-effect-free RPC latency probe.
    * `YtIngress.Bootstrapper` - ensures the cluster-singleton Watcher runs.
    * HTTP health surface (/healthz, /readyz, /status) for Kubernetes probes.
  """

  use Application

  alias YtIngress.Config
  alias YtIngress.Config.YouTube, as: YouTubeConfig

  # Shared queue group for the admin-plane request-reply endpoints: exactly
  # one replica answers each request; any replica can, via the Horde registry.
  @admin_queue "yt-ingress-admin"

  @impl true
  def start(_type, _args) do
    children =
      if Application.get_env(:yt_ingress, :server, true) do
        server_children()
      else
        []
      end

    Supervisor.start_link(children, strategy: :one_for_one, name: YtIngress.Supervisor)
  end

  # Runs on SIGTERM before the supervision tree stops: hand every local chat
  # session to a surviving node so a rolling deploy keeps streams served
  # without a discovery gap.
  @impl true
  def prep_stop(state) do
    if Application.get_env(:yt_ingress, :server, true), do: YtIngress.Drain.run()
    state
  end

  defp server_children do
    [
      {Cluster.Supervisor, [Config.cluster_topologies(), [name: YtIngress.ClusterSupervisor]]},
      {Horde.Registry, [name: YtIngress.Registry, keys: :unique, members: :auto]},
      {Horde.DynamicSupervisor,
       [
         name: YtIngress.ChatSupervisor,
         strategy: :one_for_one,
         members: :auto,
         # :passive — sessions move only when their node dies, never to
         # rebalance on a join. Same reasoning as the Twitch ingress:
         # :active stop-then-starts every moved session on every scale-up,
         # and races the drain handoff during rollouts. Balance comes from
         # deterministic placement at start time instead.
         process_redistribution: :passive,
         distribution_strategy: YtIngress.ChatDistribution
       ]},
      # RPC plane (:gnat): the yt_ingress account, on the node-local leaf.
      # Carries the admin/health RPC endpoints, the cache-invalidation
      # consumer and the broadcaster-status + token requests.
      connection_child(:nats_connection, :gnat, Config.nats()),
      # BUS plane (:gnat_bus): the shared BUS account, connected DIRECT to the
      # hub since it carries only the youtube.ingress.* publishes captured by
      # the hub's JetStream streams. Kept separate so ingress holds no
      # JetStream/event-plane rights on its RPC account.
      connection_child(:nats_bus_connection, :gnat_bus, Config.nats_bus()),
      # Counter instrumentation is batched through scheduler-friendly ETS so
      # New Relic never receives one call per event.
      YtIngress.Metrics,
      {Task.Supervisor, name: YtIngress.BroadcasterCache.TaskSupervisor},
      {Task.Supervisor, name: YtIngress.TokenSource.TaskSupervisor},
      {Task.Supervisor, name: YtIngress.ChatSession.TaskSupervisor},
      YtIngress.BroadcasterCache,
      YtIngress.TokenSource,
      {GRPC.Client.Supervisor, []},
      consumer_child(:invalidation_consumer, YtIngress.CacheInvalidator, Config.invalidation_subject()),
      # Request-reply endpoints for the admin tool / latency panel.
      rpc_consumer_child(:admin_consumer, YtIngress.AdminRpc, YouTubeConfig.admin_subject(),
        queue_group: @admin_queue
      ),
      rpc_consumer_child(:health_consumer, YtIngress.HealthRpc, YouTubeConfig.rpc_health_subject(),
        queue_group: @admin_queue
      ),
      YtIngress.Bootstrapper,
      # HTTP health surface (/healthz, /readyz, /status) for Kubernetes probes
      # and the Better Stack status page, TLS-terminated with the cert-manager
      # cert when the TLS_CERT_FILE/TLS_KEY_FILE pair is set. Last on purpose:
      # it must not answer before the planes it reports on have started.
      status_listener()
    ]
  end

  # Same contract as the Go services' pkg/health.Serve: both env vars set
  # serves HTTPS, both empty serves plaintext (local/dev), and a half-set pair
  # is a boot failure rather than a silent plaintext fallback. Rotation needs
  # no restart: Erlang's ssl pem cache re-reads the certfile when it changes.
  defp status_listener do
    port = String.to_integer(System.get_env("STATUS_PORT", "8080"))
    base = [plug: YtIngress.StatusPlug, port: port]

    case {System.get_env("TLS_CERT_FILE", ""), System.get_env("TLS_KEY_FILE", "")} do
      {"", ""} ->
        {Bandit, [{:scheme, :http} | base]}

      {cert, key} when cert != "" and key != "" ->
        {Bandit, [{:scheme, :https}, {:certfile, cert}, {:keyfile, key} | base]}

      _half_set ->
        raise "TLS_CERT_FILE and TLS_KEY_FILE must both be set or both empty"
    end
  end

  # connection_child builds a Gnat.ConnectionSupervisor child spec for one NATS
  # plane (RPC or BUS), keyed by id and registered under name.
  defp connection_child(id, name, connection_settings) do
    Supervisor.child_spec(
      {Gnat.ConnectionSupervisor,
       %{name: name, backoff_period: 4_000, connection_settings: connection_settings}},
      id: id
    )
  end

  # consumer_child builds a Gnat.ConsumerSupervisor child spec subscribing
  # module to topic on the RPC connection. opts may carry :queue_group for the
  # admin-plane endpoints; the plain invalidation consumer passes none.
  defp consumer_child(id, module, topic, opts \\ []) do
    consumer_topics_child(id, module, [topic], opts)
  end

  defp rpc_consumer_child(id, module, topic, opts) do
    consumer_topics_child(id, module, YtIngress.Rpc.subjects(topic), opts)
  end

  defp consumer_topics_child(id, module, topics, opts) do
    subscriptions = Enum.map(topics, &Enum.into(opts, %{topic: &1}))

    Supervisor.child_spec(
      {Gnat.ConsumerSupervisor,
       %{connection_name: :gnat, module: module, subscription_topics: subscriptions}},
      id: id
    )
  end
end
