# Run with the same scheduler count as the production pod:
#   export ERL_FLAGS='+S 2:2 +SDcpu 2:2 +SDio 2 +sbwt short +sbwtdcpu none +sbwtdio none'
#   MIX_ENV=test mix run bench/hot_path.exs

alias Ingress.Dispatcher

events = String.to_integer(System.get_env("INGRESS_BENCH_EVENTS", "500000"))
producers = String.to_integer(System.get_env("INGRESS_BENCH_PRODUCERS", "8"))
workers = String.to_integer(System.get_env("INGRESS_BENCH_WORKERS", "512"))

{:ok, _metrics} = Ingress.Metrics.start_link(flush_ms: 60_000)
completed = :atomics.new(1, signed: false)
handler = fn _payload, _meta -> :atomics.add(completed, 1, 1) end

{:ok, supervisor} =
  Ingress.Dispatcher.Supervisor.start_link(
    max_running: workers,
    max_queue: events,
    max_per_broadcaster: events,
    handler: handler
  )

deadline = System.monotonic_time(:millisecond) + 5_000

wait_ready = fn wait_ready ->
  ready =
    Dispatcher.worker_names(Dispatcher)
    |> Tuple.to_list()
    |> Enum.all?(&(Process.whereis(&1) != nil))

  cond do
    ready -> :ok
    System.monotonic_time(:millisecond) >= deadline -> raise "dispatcher workers did not start"
    true -> Process.sleep(5) && wait_ready.(wait_ready)
  end
end

wait_ready.(wait_ready)

per_producer = div(events, producers)
remainder = rem(events, producers)
started = System.monotonic_time(:microsecond)

tasks =
  for producer <- 0..(producers - 1) do
    count = per_producer + if(producer < remainder, do: 1, else: 0)

    Task.async(fn ->
      for sequence <- 1..count do
        broadcaster = Integer.to_string(rem(sequence + producer * 1_009, 10_000))

        Dispatcher.dispatch(%{sequence: sequence}, %{
          shard_id: producer,
          broadcaster_id: broadcaster
        })
      end
    end)
  end

Enum.each(tasks, &Task.await(&1, :infinity))
enqueued = System.monotonic_time(:microsecond)

wait_drained = fn wait_drained ->
  if :atomics.get(completed, 1) == events and Dispatcher.admitted_count() == 0 do
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

IO.puts("events=#{events} producers=#{producers} workers=#{workers}")
IO.puts("enqueue_eps=#{round(events / enqueue_seconds)}")
IO.puts("completed_eps=#{round(events / total_seconds)}")
IO.puts("elapsed_ms=#{round(total_seconds * 1_000)}")

Supervisor.stop(supervisor)
