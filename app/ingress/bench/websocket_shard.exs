# Benchmarks one real Ingress.ShardSession from a local Twitch-shaped
# WebSocket through dispatcher handoff. NATS and the downstream event pipeline
# are deliberately replaced by a counter so this measures the per-shard
# socket/read/decode/admit ceiling, not broker throughput.
#
# Run with the production scheduler shape:
#   export ERL_FLAGS='+S 2:2 +SDcpu 2:2 +SDio 2 +sbwt short +sbwtdcpu none +sbwtdio none'
#   MIX_ENV=test mix run --no-start bench/websocket_shard.exs

defmodule Ingress.WebsocketShardBench.Server do
  @websocket_guid "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

  def start(parent, opts \\ []) do
    transport = if Keyword.get(opts, :tls, false), do: :ssl, else: :gen_tcp
    {:ok, listener} = listen(transport, opts)
    {:ok, {_address, port}} = sockname(transport, listener)
    pid = spawn_link(fn -> accept(transport, listener, parent) end)
    {pid, port}
  end

  defp listen(:gen_tcp, _opts) do
    :gen_tcp.listen(0, [
      :binary,
      packet: :raw,
      active: false,
      reuseaddr: true,
      nodelay: true,
      ip: {127, 0, 0, 1}
    ])
  end

  defp listen(:ssl, opts) do
    :ssl.listen(0, [
      :binary,
      packet: :raw,
      active: false,
      reuseaddr: true,
      nodelay: true,
      ip: {127, 0, 0, 1},
      versions: [:"tlsv1.3"],
      certfile: Keyword.fetch!(opts, :certfile),
      keyfile: Keyword.fetch!(opts, :keyfile)
    ])
  end

  defp accept(transport, listener, parent) do
    {:ok, socket} = accept_socket(transport, listener)
    :ok = close(transport, listener)
    request = receive_headers(transport, socket, "")
    key = request |> header("sec-websocket-key") |> String.trim()
    accept = :crypto.hash(:sha, key <> @websocket_guid) |> Base.encode64()

    :ok =
      send_data(transport, socket, [
        "HTTP/1.1 101 Switching Protocols\r\n",
        "upgrade: websocket\r\n",
        "connection: Upgrade\r\n",
        "sec-websocket-accept: ",
        accept,
        "\r\n\r\n"
      ])

    send(parent, {:websocket_server_ready, self()})
    command_loop(transport, socket, parent)
  end

  defp accept_socket(:gen_tcp, listener), do: :gen_tcp.accept(listener)

  defp accept_socket(:ssl, listener) do
    with {:ok, socket} <- :ssl.transport_accept(listener, 5_000),
         {:ok, socket} <- :ssl.handshake(socket, 5_000) do
      {:ok, socket}
    end
  end

  defp sockname(:gen_tcp, socket), do: :inet.sockname(socket)
  defp sockname(:ssl, socket), do: :ssl.sockname(socket)

  defp receive_headers(transport, socket, acc) do
    case :binary.match(acc, "\r\n\r\n") do
      :nomatch ->
        {:ok, chunk} = recv(transport, socket, 0, 5_000)
        receive_headers(transport, socket, acc <> chunk)

      {_offset, _length} ->
        acc
    end
  end

  defp header(request, name) do
    prefix = String.downcase(name) <> ":"

    request
    |> String.split("\r\n")
    |> Enum.find_value(fn line ->
      downcased = String.downcase(line)

      if String.starts_with?(downcased, prefix),
        do: binary_part(line, byte_size(prefix), byte_size(line) - byte_size(prefix))
    end)
    |> case do
      nil -> raise "missing #{name} in websocket upgrade"
      value -> value
    end
  end

  defp command_loop(transport, socket, parent) do
    receive do
      {:send_frames, ref, frame, count, batch_size} ->
        started = System.monotonic_time(:microsecond)
        result = send_frames(transport, socket, frame, count, batch_size)
        duration_us = System.monotonic_time(:microsecond) - started
        send(parent, {:websocket_server_sent, ref, result, duration_us})
        command_loop(transport, socket, parent)

      :stop ->
        close(transport, socket)
        :ok
    end
  end

  defp send_frames(_transport, _socket, _frame, 0, _batch_size), do: :ok

  defp send_frames(transport, socket, frame, remaining, batch_size) do
    batch_count = min(remaining, batch_size)

    case send_data(transport, socket, List.duplicate(frame, batch_count)) do
      :ok -> send_frames(transport, socket, frame, remaining - batch_count, batch_size)
      {:error, _reason} = error -> error
    end
  end

  defp recv(:gen_tcp, socket, length, timeout), do: :gen_tcp.recv(socket, length, timeout)
  defp recv(:ssl, socket, length, timeout), do: :ssl.recv(socket, length, timeout)
  defp send_data(:gen_tcp, socket, data), do: :gen_tcp.send(socket, data)
  defp send_data(:ssl, socket, data), do: :ssl.send(socket, data)
  defp close(:gen_tcp, socket), do: :gen_tcp.close(socket)
  defp close(:ssl, socket), do: :ssl.close(socket)
end

defmodule Ingress.WebsocketShardBench do
  alias Ingress.{Dispatcher, ShardSession}
  alias Ingress.WebsocketShardBench.Server

  def run do
    {:ok, _} = Application.ensure_all_started(:mint_web_socket)
    {:ok, _} = Application.ensure_all_started(:new_relic_agent)

    events = env_integer("INGRESS_WS_BENCH_EVENTS", 500_000)
    warmup = env_integer("INGRESS_WS_BENCH_WARMUP", 25_000)
    batch_size = env_integer("INGRESS_WS_BENCH_BATCH_SIZE", 64)
    workers = env_integer("INGRESS_WS_BENCH_WORKERS", 512)
    tls? = System.get_env("INGRESS_WS_BENCH_TLS", "false") == "true"
    cafile = System.get_env("INGRESS_WS_BENCH_CAFILE")
    certfile = System.get_env("INGRESS_WS_BENCH_CERTFILE")
    keyfile = System.get_env("INGRESS_WS_BENCH_KEYFILE")
    completed = :atomics.new(1, signed: false)
    handler = fn _payload, _meta -> :atomics.add(completed, 1, 1) end

    {:ok, metrics} = Ingress.Metrics.start_link(flush_ms: 60_000)

    {:ok, dispatcher_supervisor} =
      Ingress.Dispatcher.Supervisor.start_link(
        max_running: workers,
        max_queue: events + warmup,
        max_per_broadcaster: events + warmup,
        completion_batch_size: 4,
        completion_flush_ms: 25,
        handler: handler
      )

    wait_workers(workers)
    parent = self()
    server_opts = if tls?, do: [tls: true, certfile: certfile, keyfile: keyfile], else: []
    {server, port} = Server.start(parent, server_opts)
    scheme = if tls?, do: "wss", else: "ws"
    Application.put_env(:ingress, :eventsub_url, "#{scheme}://127.0.0.1:#{port}/ws")
    ws_connect_opts = if tls?, do: [transport_opts: [cacertfile: cafile]], else: []

    {:ok, shard} =
      ShardSession.start_link(
        shard_id: 0,
        conduit_id: "websocket-benchmark",
        rescue?: true,
        ws_connect_opts: ws_connect_opts
      )

    receive do
      {:websocket_server_ready, ^server} -> :ok
    after
      5_000 -> raise "websocket server handshake timed out"
    end

    wait_upgraded(shard)
    cancel_welcome_deadline(shard)

    payload = notification_payload()
    frame = websocket_text_frame(payload)

    {_warmup_us, warmup_stats} =
      drive(server, shard, completed, frame, warmup, batch_size, warmup)

    wait_dispatcher_drained()
    reductions_before = process_reductions(shard)
    memory_before = process_memory(shard)

    {duration_us, stats} =
      drive(server, shard, completed, frame, events, batch_size, warmup + events)

    wait_dispatcher_drained()
    reductions = process_reductions(shard) - reductions_before
    memory_delta = process_memory(shard) - memory_before
    status = ShardSession.status(shard)
    sender_duration_us = receive_sender_duration(stats.ref)

    result = %{
      events: events,
      warmup_events: warmup,
      payload_bytes: byte_size(payload),
      websocket_frame_bytes: byte_size(frame),
      socket_batch_frames: batch_size,
      workers: workers,
      schedulers: System.schedulers_online(),
      dirty_cpu_schedulers: :erlang.system_info(:dirty_cpu_schedulers),
      tls: tls?,
      receiver_events_per_second: round(events * 1_000_000 / duration_us),
      sender_events_per_second: round(events * 1_000_000 / sender_duration_us),
      duration_ms: Float.round(duration_us / 1_000, 3),
      sender_duration_ms: Float.round(sender_duration_us / 1_000, 3),
      shard_load_window_count: status.load,
      dispatcher_completed: :atomics.get(completed, 1) - warmup,
      dispatcher_pending: Dispatcher.admitted_count(),
      shard_max_mailbox: stats.max_mailbox,
      warmup_max_mailbox: warmup_stats.max_mailbox,
      max_run_queue: stats.max_run_queue,
      shard_reductions_per_event: Float.round(reductions / events, 2),
      shard_memory_delta_bytes: memory_delta
    }

    IO.puts("WEBSOCKET_SHARD_RESULT=" <> Jason.encode!(result))

    send(server, :stop)
    GenServer.stop(shard)
    Supervisor.stop(dispatcher_supervisor)
    GenServer.stop(metrics)
  end

  defp drive(server, shard, completed, frame, count, batch_size, completed_target) do
    ref = make_ref()
    started = System.monotonic_time(:microsecond)
    send(server, {:send_frames, ref, frame, count, batch_size})

    stats =
      wait_completed(
        completed,
        completed_target,
        shard,
        System.monotonic_time(:millisecond) + 120_000,
        %{ref: ref, max_mailbox: 0, max_run_queue: 0}
      )

    {System.monotonic_time(:microsecond) - started, stats}
  end

  defp wait_completed(completed, target, shard, deadline, stats) do
    mailbox = process_info_value(shard, :message_queue_len)
    run_queue = :erlang.statistics(:run_queue)

    stats = %{
      stats
      | max_mailbox: max(stats.max_mailbox, mailbox),
        max_run_queue: max(stats.max_run_queue, run_queue)
    }

    cond do
      :atomics.get(completed, 1) >= target ->
        stats

      System.monotonic_time(:millisecond) >= deadline ->
        raise "shard benchmark timed out at #{:atomics.get(completed, 1)}/#{target} events"

      not Process.alive?(shard) ->
        raise "shard exited during benchmark"

      true ->
        Process.sleep(1)
        wait_completed(completed, target, shard, deadline, stats)
    end
  end

  defp receive_sender_duration(ref) do
    receive do
      {:websocket_server_sent, ^ref, :ok, duration_us} ->
        duration_us

      {:websocket_server_sent, ^ref, {:error, reason}, _duration_us} ->
        raise "websocket sender failed: #{inspect(reason)}"
    after
      5_000 -> raise "websocket sender result timed out"
    end
  end

  defp wait_workers(expected) do
    wait_until(fn ->
      Dispatcher.worker_names(Dispatcher)
      |> Tuple.to_list()
      |> Enum.count(&(Process.whereis(&1) != nil))
      |> Kernel.==(expected)
    end)
  end

  defp wait_upgraded(shard) do
    wait_until(fn ->
      case :sys.get_state(shard) do
        %{primary: %{websocket: websocket}} when not is_nil(websocket) -> true
        _ -> false
      end
    end)
  end

  defp cancel_welcome_deadline(shard) do
    :sys.replace_state(shard, fn state ->
      if state.welcome_timer, do: Process.cancel_timer(state.welcome_timer)
      %{state | welcome_timer: nil}
    end)
  end

  defp wait_dispatcher_drained do
    wait_until(fn -> Dispatcher.admitted_count() == 0 end, 30_000)
  end

  defp wait_until(fun, timeout_ms \\ 10_000) do
    deadline = System.monotonic_time(:millisecond) + timeout_ms
    do_wait_until(fun, deadline, timeout_ms)
  end

  defp do_wait_until(fun, deadline, timeout_ms) do
    cond do
      fun.() ->
        :ok

      System.monotonic_time(:millisecond) >= deadline ->
        raise "condition not met within #{timeout_ms}ms"

      true ->
        Process.sleep(2)
        do_wait_until(fun, deadline, timeout_ms)
    end
  end

  defp websocket_text_frame(payload) do
    size = byte_size(payload)

    cond do
      size <= 125 -> <<0x81, size, payload::binary>>
      size <= 65_535 -> <<0x81, 126, size::16, payload::binary>>
      true -> <<0x81, 127, size::64, payload::binary>>
    end
  end

  defp notification_payload do
    Ingress.JSON.encode(%{
      "metadata" => %{
        "message_id" => "01JZWEBSOCKETBENCHMARK000001",
        "message_type" => "notification",
        "message_timestamp" => "2026-07-15T12:00:00.000000000Z",
        "subscription_type" => "channel.chat.message",
        "subscription_version" => "1"
      },
      "payload" => %{
        "subscription" => %{
          "id" => "benchmark-subscription",
          "status" => "enabled",
          "type" => "channel.chat.message",
          "version" => "1",
          "condition" => %{"broadcaster_user_id" => "123456"},
          "transport" => %{"method" => "websocket", "session_id" => "benchmark-session"},
          "created_at" => "2026-07-15T12:00:00.000000000Z",
          "cost" => 0
        },
        "event" => %{
          "broadcaster_user_id" => "123456",
          "broadcaster_user_login" => "benchmark_channel",
          "broadcaster_user_name" => "Benchmark Channel",
          "chatter_user_id" => "654321",
          "chatter_user_login" => "benchmark_chatter",
          "chatter_user_name" => "Benchmark Chatter",
          "message_id" => "benchmark-message",
          "message" => %{
            "text" => "A representative Twitch chat message for the ingress WebSocket benchmark",
            "fragments" => [
              %{
                "type" => "text",
                "text" =>
                  "A representative Twitch chat message for the ingress WebSocket benchmark",
                "cheermote" => nil,
                "emote" => nil,
                "mention" => nil
              }
            ]
          },
          "color" => "#52B788",
          "badges" => [%{"set_id" => "subscriber", "id" => "12", "info" => "12"}],
          "message_type" => "text",
          "cheer" => nil,
          "reply" => nil,
          "channel_points_custom_reward_id" => nil,
          "source_broadcaster_user_id" => nil,
          "source_broadcaster_user_login" => nil,
          "source_broadcaster_user_name" => nil,
          "source_message_id" => nil,
          "source_badges" => nil
        }
      }
    })
    |> IO.iodata_to_binary()
  end

  defp process_reductions(pid), do: process_info_value(pid, :reductions)
  defp process_memory(pid), do: process_info_value(pid, :memory)

  defp process_info_value(pid, key) do
    {^key, value} = Process.info(pid, key)
    value
  end

  defp env_integer(name, default) do
    name |> System.get_env(Integer.to_string(default)) |> String.to_integer()
  end
end

Ingress.WebsocketShardBench.run()
