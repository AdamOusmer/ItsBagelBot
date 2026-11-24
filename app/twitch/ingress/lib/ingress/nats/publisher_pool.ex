# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats.PublisherPool do
  use Supervisor

  alias Ingress.Config
  alias Ingress.Config.Publish, as: PublishConfig

  @spec start_link(term()) :: Supervisor.on_start()
  def start_link(_opts), do: Supervisor.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init(_opts) do
    n = PublishConfig.connections()
    :persistent_term.put({Ingress.Nats.Publisher, :n}, n)

    children =
      Enum.flat_map(0..(n - 1), fn i ->
        conn = connection_name(i)

        [
          Platform.Nats.connection_child({:pub_conn, i}, conn, Config.nats_bus()),
          Supervisor.child_spec(
            {Ingress.Nats.Publisher, [index: i, conn: conn]},
            id: {:publisher, i}
          )
        ]
      end)

    Supervisor.init(children, strategy: :one_for_one)
  end

  @doc false
  def connection_name(index), do: :"gnat_bus_pub_#{index}"

  @doc "Hub connections for Platform.Nats.HubFailback: the BUS connection, plus each publisher shard when the pool can spill."
  def hub_failback_connections do
    pool =
      case :persistent_term.get({Ingress.Nats.Publisher, :n}, 0) do
        n when n > 1 ->
          for index <- 0..(n - 1),
              do: {connection_name(index), fn -> Ingress.Nats.Publisher.failback(index) end}

        _ ->
          []
      end

    [{:gnat_bus, fn -> Platform.Nats.stop(:gnat_bus) end} | pool]
  end
end
