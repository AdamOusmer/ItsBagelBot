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
    * `Ingress.Squash` - coalesces identical non-command chat into one folded
      `channel.chat.message` carrying every sender, so duplicates keep their
      reputation/campaign signal without one bus event each.
    * `Ingress.Dispatcher` - bounded async notification filtering and NATS
      publish workers so shard socket processes never do that work inline.
    * `Ingress.Twitch.AppToken` - cached app access token for Helix calls.
    * `Gnat.ConnectionSupervisor` - RPC-plane NATS connection (twitch_ingress
      account), registered as `:gnat`.
    * `Gnat.ConnectionSupervisor` - BUS-plane NATS connection (shared BUS
      account), registered as `:gnat_bus`; carries the twitch.ingress.* firehose.
    * `Ingress.Nats.PublisherPool` - a pool of asynchronous, pipelined JetStream
      publishers + ack multiplexers for the lane firehose, sharded across N BUS
      connections so publish throughput scales past one connection's process
      ceiling and is bounded by the in-flight window, not one blocked worker per
      PubAck.
    * `Gnat.ConsumerSupervisor` (invalidation) - subscription to cache
      invalidation subject.
    * `Gnat.ConsumerSupervisor` (admin) - request-reply read endpoint for
      `Ingress.AdminRpc`.
    * `Gnat.ConsumerSupervisor` (scale) - request-reply control endpoint for
      `Ingress.ScaleRpc` (manual shard count).
    * `Gnat.ConsumerSupervisor` (autoscale) - request-reply control endpoint
      for `Ingress.AutoscaleRpc` (load-based autoscaler toggle).
    * `Ingress.Bootstrapper` - ensures the cluster-singleton ShardScaler and
      ConduitManager run.
  """

  use Application

  alias Ingress.Config

  # Shared queue group for the admin-plane request-reply endpoints: exactly one
  # replica answers each request; any replica can, via the Horde registry.
  @admin_queue "twitch-ingress-admin"

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
      # RPC plane (:gnat): the twitch_ingress account, on the node-local leaf.
      # Carries the admin/scale/autoscale/conduit RPC endpoints, the
      # cache-invalidation consumer and the broadcaster-status request.
      connection_child(:nats_connection, :gnat, Config.nats()),
      # BUS plane (:gnat_bus): the shared BUS account, connected DIRECT to the hub
      # (not the leaf) since it carries only the twitch.ingress.* firehose
      # publishes (Ingress.Nats) captured by the hub's JetStream streams. Kept a
      # separate account so ingress holds no JetStream/event-plane rights on its
      # RPC account. Ingress.Nats.PublisherPool opens further hub connections for
      # the acked firehose; this one carries the fire-and-forget status publishes.
      connection_child(:nats_bus_connection, :gnat_bus, Config.nats_bus()),
      Ingress.NatsFailback,
      # Sharded asynchronous ack multiplexer for lane publishes
      # (Ingress.Nats.publish_acked): a pool of BUS connections + collectors so
      # publish throughput scales past a single connection's process ceiling.
      # Must start before the Dispatcher and Squash, which publish through it.
      Ingress.Nats.PublisherPool,
      {Task.Supervisor, name: Ingress.BroadcasterCache.TaskSupervisor},
      # Runs squash-cohort publishes (JetStream-acked) off the Squash process,
      # so a slow broker never stalls the sweep.
      {Task.Supervisor, name: Ingress.PublishSupervisor},
      Ingress.BroadcasterCache,
      Ingress.Squash,
      Ingress.Dispatcher.Supervisor,
      Ingress.Twitch.AppToken,
      consumer_child(
        :invalidation_consumer,
        Ingress.CacheInvalidator,
        Config.invalidation_subject()
      ),
      # Request-reply endpoint for the admin tool.
      consumer_child(:admin_consumer, Ingress.AdminRpc, Config.admin_subject(),
        queue_group: @admin_queue
      ),
      # Manual shard scaling: {"count": N}.
      consumer_child(:scale_consumer, Ingress.ScaleRpc, Config.scale_subject(),
        queue_group: @admin_queue
      ),
      # Toggle the load-based autoscaler: {"enabled": bool}.
      consumer_child(:autoscale_consumer, Ingress.AutoscaleRpc, Config.autoscale_subject(),
        queue_group: @admin_queue
      ),
      # Live conduit id: body {}, replies {"conduit_id": "<uuid>"}.
      consumer_child(:conduit_consumer, Ingress.ConduitRpc, Config.conduit_subject(),
        queue_group: @admin_queue
      ),
      Ingress.Bootstrapper
    ]
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

  # consumer_child builds a Gnat.ConsumerSupervisor child spec subscribing module
  # to topic on the RPC connection. opts may carry :queue_group for the admin-
  # plane endpoints; the plain invalidation consumer passes none.
  defp consumer_child(id, module, topic, opts \\ []) do
    subscription = Enum.into(opts, %{topic: topic})

    Supervisor.child_spec(
      {Gnat.ConsumerSupervisor,
       %{connection_name: :gnat, module: module, subscription_topics: [subscription]}},
      id: id
    )
  end
end
