defmodule Ingress.Nats.PublisherPool do
  @moduledoc """
  Supervises the scheduler-local batch lane publisher: `publish_connections`
  independent BUS connections, each paired with one
  `Ingress.Nats.Publisher` batcher/collector.

  Every `Gnat.pub` is serialized by one connection process, so the pool spreads
  writes across online schedulers. Within each shard, concurrent events share
  an atomic commit PubAck, removing the one-ack-per-event collector bottleneck.

  The pool records the shard count in `:persistent_term` before any collector
  starts, so `Ingress.Nats.Publisher.enqueue/3` — invoked from dispatcher and
  squash workers the moment they come up — can route to a shard even
  during a partial pool restart (a shard whose context is not yet present just
  reports `:not_connected`, and the caller drops that one event).

  Each shard's BUS connection uses the same shared-BUS credentials as the
  status-plane `:gnat_bus` connection; they are separate TCP connections so the
  firehose never contends with status/telemetry publishes.
  """

  use Supervisor

  alias Ingress.Config

  @spec start_link(term()) :: Supervisor.on_start()
  def start_link(_opts), do: Supervisor.start_link(__MODULE__, [], name: __MODULE__)

  @impl true
  def init(_opts) do
    n = Config.publish_connections()
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

    # one_for_one: shards are independent, and a collector re-subscribes its
    # reply inbox on its own when its BUS connection flaps (it monitors the
    # connection and retries), so a connection restart never needs to cascade
    # into sibling shards.
    Supervisor.init(children, strategy: :one_for_one)
  end

  @doc false
  def connection_name(index), do: :"gnat_bus_pub_#{index}"
end
