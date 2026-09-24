# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Application do
  use Application

  alias Ingress.Config
  alias Ingress.Config.Admin, as: AdminConfig

  @admin_queue "twitch-ingress-admin"

  @impl true
  def start(_type, _args) do
    children =
      if Application.get_env(:ingress, :server, true) do
        Config.install_hot_path()
        server_children()
      else
        []
      end

    Supervisor.start_link(children, strategy: :one_for_one, name: Ingress.Supervisor)
  end

  @impl true
  def prep_stop(state) do
    if Application.get_env(:ingress, :server, true), do: Ingress.Drain.run()
    state
  end

  @impl true
  def stop(_state) do
    Config.uninstall_hot_path()
    :ok
  end

  defp server_children do
    base_children() ++
      trial_children() ++
      control_children() ++
      [Ingress.Bootstrapper, status_listener()]
  end

  defp base_children do
    [
      {Cluster.Supervisor, [Config.cluster_topologies(), [name: Ingress.ClusterSupervisor]]},
      {Horde.Registry, [name: Ingress.Registry, keys: :unique, members: :auto]},
      {Horde.DynamicSupervisor,
       [
         name: Ingress.ShardSupervisor,
         strategy: :one_for_one,
         members: :auto,
         # :active stops then restarts shards on every membership change, dropping events.
         process_redistribution: :passive,
         distribution_strategy: Ingress.ShardDistribution
       ]},
      connection_child(:nats_connection, :gnat, Config.nats()),
      connection_child(:nats_bus_connection, :gnat_bus, Config.nats_bus()),
      Ingress.NatsFailback,
      Ingress.Metrics,
      # Must start before the Dispatcher and Squash, which publish through it.
      Ingress.Nats.PublisherPool,
      {Task.Supervisor, name: Ingress.BroadcasterCache.TaskSupervisor},
      Ingress.BroadcasterCache,
      Ingress.Squash.Pool,
      Ingress.Dispatcher.Supervisor,
      Ingress.Twitch.AppToken
    ]
  end

  defp trial_children do
    [
      Ingress.TrialValkey,
      Ingress.TrialMembership,
      {Ingress.TrialReceiver, slot: 0},
      {Ingress.TrialReceiver, slot: 1},
      {Ingress.TrialReceiver, slot: 2},
      Supervisor.child_spec(
        {Gnat.ConsumerSupervisor,
         %{
           connection_name: :gnat_bus,
           module: Ingress.TrialUserChanged,
           subscription_topics: [%{topic: "data.users.changed"}]
         }},
        id: :trial_user_changed_consumer
      ),
      rpc_consumer_child(
        :trial_list_consumer,
        Ingress.TrialListRpc,
        "twitch.ingress.admin.trials.list",
        queue_group: @admin_queue
      ),
      rpc_consumer_child(
        :trial_add_consumer,
        Ingress.TrialAddRpc,
        "twitch.ingress.admin.trials.add",
        queue_group: @admin_queue
      ),
      rpc_consumer_child(
        :trial_remove_consumer,
        Ingress.TrialRemoveRpc,
        "twitch.ingress.admin.trials.remove",
        queue_group: @admin_queue
      ),
      rpc_consumer_child(
        :trial_set_enabled_consumer,
        Ingress.TrialSetEnabledRpc,
        "twitch.ingress.admin.trials.set_enabled",
        queue_group: @admin_queue
      )
    ]
  end

  defp control_children do
    [
      consumer_child(
        :invalidation_consumer,
        Ingress.CacheInvalidator,
        Config.invalidation_subject()
      ),
      rpc_consumer_child(:admin_consumer, Ingress.AdminRpc, AdminConfig.admin_subject(),
        queue_group: @admin_queue
      ),
      rpc_consumer_child(:scale_consumer, Ingress.ScaleRpc, AdminConfig.scale_subject(),
        queue_group: @admin_queue
      ),
      rpc_consumer_child(
        :autoscale_consumer,
        Ingress.AutoscaleRpc,
        AdminConfig.autoscale_subject(),
        queue_group: @admin_queue
      ),
      rpc_consumer_child(:conduit_consumer, Ingress.ConduitRpc, AdminConfig.conduit_subject(),
        queue_group: @admin_queue
      ),
      rpc_consumer_child(:health_consumer, Ingress.HealthRpc, AdminConfig.rpc_health_subject(),
        queue_group: @admin_queue
      )
    ]
  end

  defp status_listener do
    port = String.to_integer(System.get_env("STATUS_PORT", "8080"))
    base = [plug: Ingress.StatusPlug, port: port]

    case {System.get_env("TLS_CERT_FILE", ""), System.get_env("TLS_KEY_FILE", "")} do
      {"", ""} ->
        {Bandit, [{:scheme, :http} | base]}

      {cert, key} when cert != "" and key != "" ->
        {Bandit, [{:scheme, :https}, {:certfile, cert}, {:keyfile, key} | base]}

      _half_set ->
        raise "TLS_CERT_FILE and TLS_KEY_FILE must both be set or both empty"
    end
  end

  defp connection_child(id, name, connection_settings) do
    Supervisor.child_spec(
      {Gnat.ConnectionSupervisor,
       %{name: name, backoff_period: 4_000, connection_settings: connection_settings}},
      id: id
    )
  end

  defp consumer_child(id, module, topic, opts \\ []) do
    consumer_topics_child(id, module, [topic], opts)
  end

  defp rpc_consumer_child(id, module, topic, opts) do
    consumer_topics_child(id, module, Ingress.Rpc.subjects(topic), opts)
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
