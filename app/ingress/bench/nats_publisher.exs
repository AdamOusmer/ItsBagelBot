# Measures the BEAM-side publish + PubAck path without a network or broker.
# Gnat still prepares headers, publishes cohorts and emits telemetry exactly as it
# does in production; the fake connection immediately returns JetStream acks.
#
#   export ERL_FLAGS='+S 2:2 +SDcpu 2:2 +SDio 2 +sbwt short +sbwtdcpu none +sbwtdio none'
#   MIX_ENV=test mix run bench/nats_publisher.exs

defmodule BenchGnat do
  use GenServer

  def start_link(name), do: GenServer.start_link(__MODULE__, nil, name: name)
  def init(nil), do: {:ok, %{receiver: nil, sid: 0}}

  def handle_call({:sub, receiver, _topic, _opts}, _from, state) do
    sid = state.sid + 1
    {:reply, {:ok, sid}, %{state | receiver: receiver, sid: sid}}
  end

  def handle_call({:pub, _topic, _message, opts}, _from, state) do
    send(state.receiver, {:msg, %{topic: Keyword.fetch!(opts, :reply_to), body: ~s({"seq":1})}})
    {:reply, :ok, state}
  end
end

alias Ingress.Nats.Publisher

events = String.to_integer(System.get_env("INGRESS_BENCH_EVENTS", "300000"))
producers = String.to_integer(System.get_env("INGRESS_BENCH_PRODUCERS", "8"))
connections = String.to_integer(System.get_env("INGRESS_BENCH_CONNECTIONS", "2"))

Application.put_env(:ingress, :publish_max_pending, 65_536)
{:ok, metrics} = Ingress.Metrics.start_link(flush_ms: 60_000)

contexts =
  for index <- 0..(connections - 1) do
    conn = String.to_atom("bench_gnat_#{index}")
    {:ok, _conn} = BenchGnat.start_link(conn)
    {:ok, _publisher} = Publisher.start_link(index: index, conn: conn)
    :persistent_term.get({Publisher, :ctx, index})
  end

:persistent_term.put({Publisher, :n}, connections)
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
        :ok = Publisher.enqueue("twitch.ingress.event.standard", ~s({"type":"chat"}), id)
      end
    end)
  end

Enum.each(tasks, &Task.await(&1, :infinity))
enqueued = System.monotonic_time(:microsecond)

wait_drained = fn wait_drained ->
  if Enum.all?(contexts, &(:atomics.get(&1.counter, 1) == 0)) do
    :ok
  else
    Process.sleep(1)
    wait_drained.(wait_drained)
  end
end

wait_drained.(wait_drained)
finished = System.monotonic_time(:microsecond)
enqueue_seconds = (enqueued - started) / 1_000_000
total_seconds = (finished - started) / 1_000_000

IO.puts("events=#{events} producers=#{producers} connections=#{connections}")
IO.puts("enqueue_eps=#{round(events / enqueue_seconds)}")
IO.puts("acked_eps=#{round(events / total_seconds)}")
IO.puts("elapsed_ms=#{round(total_seconds * 1_000)}")

GenServer.stop(metrics)
