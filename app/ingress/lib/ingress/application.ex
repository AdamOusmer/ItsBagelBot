defmodule Ingress.Application do
  @moduledoc """
  Top-level supervisor for the Twitch ingress.

  Tree (`:one_for_one`, per the twitch-ingress doc page):

    * `Cluster.Supervisor` (libcluster) - BEAM node auto-discovery.
    * `Ingress.Registry` (Horde) - cluster-wide `{:shard, id} -> pid` registry.
    * `Ingress.ShardSupervisor` (Horde) - spawns shard sessions, re-assigns
      them to surviving nodes when a node leaves.
    * `Ingress.BroadcasterCache` - in-process ETS cache over the broadcaster
      status NATS RPC (the ingress never reads the database directly).
    * `Ingress.Twitch.AppToken` - cached app access token for Helix calls.
    * `Gnat.ConnectionSupervisor` - NATS connection, registered as `:gnat`.
    * `Gnat.ConsumerSupervisor` - subscription to the cache-invalidation subject.
    * `Ingress.Bootstrapper` - ensures the cluster-singleton ConduitManager runs.
  """

  use Application

  alias Ingress.Config

  @impl true
  def start(_type, _args) do
    children =
      if Application.get_env(:ingress, :server, true) do
        server_children()
      else
        []
      end

    Supervisor.start_link(children, strategy: :one_for_one, name: Ingress.Supervisor)
  end

  defp server_children do
    [
      {Cluster.Supervisor, [Config.cluster_topologies(), [name: Ingress.ClusterSupervisor]]},
      {Horde.Registry, [name: Ingress.Registry, keys: :unique, members: :auto]},
      {Horde.DynamicSupervisor,
       [
         name: Ingress.ShardSupervisor,
         strategy: :one_for_one,
         members: :auto,
         process_redistribution: :active,
         # Round-robin shards across nodes (3/2 on a two-node fleet) instead
         # of the default hash ring, which clusters them (4/1 observed).
         distribution_strategy: Ingress.ShardDistribution
       ]},
      nats_connection(),
      Ingress.BroadcasterCache,
      Ingress.Twitch.AppToken,
      invalidation_consumer(),
      admin_consumer(),
      Ingress.Bootstrapper
    ]
  end

  defp nats_connection do
    %{host: host, port: port} = Config.nats()

    settings = %{
      name: :gnat,
      backoff_period: 4_000,
      connection_settings: [%{host: host, port: port}]
    }

    Supervisor.child_spec(
      {Gnat.ConnectionSupervisor, settings},
      id: :nats_connection
    )
  end

  defp invalidation_consumer do
    settings = %{
      connection_name: :gnat,
      module: Ingress.CacheInvalidator,
      subscription_topics: [%{topic: Config.invalidation_subject()}]
    }

    Supervisor.child_spec(
      {Gnat.ConsumerSupervisor, settings},
      id: :invalidation_consumer
    )
  end

  # Request-reply endpoint for the admin tool. Queue group so exactly one
  # replica answers each request; any replica can, via the Horde registry.
  defp admin_consumer do
    settings = %{
      connection_name: :gnat,
      module: Ingress.AdminRpc,
      subscription_topics: [%{topic: Config.admin_subject(), queue_group: "twitch-ingress-admin"}]
    }

    Supervisor.child_spec(
      {Gnat.ConsumerSupervisor, settings},
      id: :admin_consumer
    )
  end
end
