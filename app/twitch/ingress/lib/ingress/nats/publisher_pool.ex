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
          Supervisor.child_spec(
            {Gnat.ConnectionSupervisor,
             %{name: conn, backoff_period: 4_000, connection_settings: Config.nats_bus()}},
            id: {:pub_conn, i}
          ),
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
end
