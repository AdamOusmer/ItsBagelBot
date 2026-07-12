# Exercises the complete cached chat path: dispatcher admission, lane routing,
# duplicate-window insert, JSON encoding, Gnat header preparation, cohort publish
# tracking and PubAck reconciliation. Only the kernel socket and broker storage
# are replaced by an immediate in-VM PubAck.
#
#   export ERL_FLAGS='+S 2:2 +SDcpu 2:2 +SDio 2 +sbwt short +sbwtdcpu none +sbwtdio none'
#   MIX_ENV=test mix run bench/end_to_end.exs

defmodule EndToEndBenchGnat do
  use GenServer

  def start_link(name), do: GenServer.start_link(__MODULE__, nil, name: name)
  def init(nil), do: {:ok, %{receiver: nil, sid: 0}}

  def handle_call({:sub, receiver, _topic, _opts}, _from, state) do
    sid = state.sid + 1
    {:reply, {:ok, sid}, %{state | receiver: receiver, sid: sid}}
  end

  def handle_call({:pub, _topic, _message, opts}, _from, state) do
    if reply = Keyword.get(opts, :reply_to) do
      send(state.receiver, {:msg, %{topic: reply, body: ~s({"seq":1})}})
    end

    {:reply, :ok, state}
  end
end

alias Ingress.{BroadcasterCache, Dispatcher, Nats.Publisher}

events = String.to_integer(System.get_env("INGRESS_BENCH_EVENTS", "100000"))
producers = String.to_integer(System.get_env("INGRESS_BENCH_PRODUCERS", "8"))
connections = String.to_integer(System.get_env("INGRESS_BENCH_CONNECTIONS", "2"))

squash_partitions =
  String.to_integer(
    System.get_env("INGRESS_BENCH_SQUASH_PARTITIONS", Integer.to_string(connections))
  )

workers = String.to_integer(System.get_env("INGRESS_BENCH_WORKERS", "512"))
direct = System.get_env("INGRESS_BENCH_DIRECT", "false") == "true"
bypass_squash = System.get_env("INGRESS_BENCH_BYPASS_SQUASH", "false") == "true"

Application.put_env(:ingress, :publish_max_pending, 65_536)
Ingress.Config.install_hot_path()
{:ok, _metrics} = Ingress.Metrics.start_link(flush_ms: 60_000)
{:ok, _tasks} = Task.Supervisor.start_link(name: Ingress.BroadcasterCache.TaskSupervisor)
{:ok, _cache} = BroadcasterCache.start_link(loader: fn _ -> {:ok, :standard} end)
{:ok, _squash} = Ingress.Squash.Pool.start_link(partitions: squash_partitions)

contexts =
  for index <- 0..(connections - 1) do
    conn = String.to_atom("end_to_end_bench_gnat_#{index}")
    {:ok, _conn} = EndToEndBenchGnat.start_link(conn)

    {:ok, _publisher} = Publisher.start_link(index: index, conn: conn)

    :persistent_term.get({Publisher, :ctx, index})
  end

:persistent_term.put({Publisher, :n}, connections)
:standard = BroadcasterCache.lane("77")

unless direct do
  {:ok, _dispatcher} =
    Ingress.Dispatcher.Supervisor.start_link(
      max_running: workers,
      max_queue: events,
      max_per_broadcaster: events
    )
end

Process.sleep(20)
per_producer = div(events, producers)
remainder = rem(events, producers)
started = System.monotonic_time(:microsecond)

tasks =
  for producer <- 0..(producers - 1) do
    count = per_producer + if(producer < remainder, do: 1, else: 0)

    Task.async(fn ->
      for sequence <- 1..count do
        id = Integer.to_string(producer * per_producer + sequence)

        payload = %{
          "subscription" => %{"type" => "channel.chat.message"},
          "event" => %{
            "broadcaster_user_id" => "77",
            "broadcaster_user_login" => "channel",
            "chatter_user_id" => id,
            "chatter_user_login" => id,
            "message" => %{
              "text" =>
                if(bypass_squash, do: "!unique message " <> id, else: "unique message " <> id)
            },
            "badges" => []
          }
        }

        meta = %{
          shard_id: producer,
          broadcaster_id: "77",
          msg_id: id,
          ts: 0
        }

        if direct do
          Ingress.Pipeline.handle_event(payload, meta)
        else
          Dispatcher.dispatch(payload, meta)
        end
      end
    end)
  end

Enum.each(tasks, &Task.await(&1, :infinity))

wait_drained = fn wait_drained ->
  dispatched = direct or Dispatcher.admitted_count() == 0
  acked = Enum.all?(contexts, &(:atomics.get(&1.counter, 1) == 0))

  if dispatched and acked do
    :ok
  else
    Process.sleep(1)
    wait_drained.(wait_drained)
  end
end

wait_drained.(wait_drained)
finished = System.monotonic_time(:microsecond)
seconds = (finished - started) / 1_000_000

IO.puts(
  "events=#{events} producers=#{producers} workers=#{workers} connections=#{connections} squash_partitions=#{squash_partitions} direct=#{direct} bypass_squash=#{bypass_squash}"
)

IO.puts("end_to_end_eps=#{round(events / seconds)}")
IO.puts("elapsed_ms=#{round(seconds * 1_000)}")
