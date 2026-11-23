# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.Pull do
  @moduledoc "Supervised JetStream pull consumer with batch pulls, re-attach and drain support."
  use GenServer

  alias Platform.Nats
  alias Platform.Nats.{Ack, Planes}

  @required [:name, :stream, :durable, :handler, :task_supervisor]
  @defaults [plane: :jetstream, batch: 16, expires_ns: 25_000_000_000, inbox_prefix: "_INBOX"]
  @retry_ms 1_000
  @guard_slack_ms 5_000
  @paused_nak_ns 250_000_000

  def start_link(opts) do
    Enum.each(@required, &Keyword.fetch!(opts, &1))
    config = @defaults |> Keyword.merge(opts) |> Map.new()
    GenServer.start_link(__MODULE__, config, name: config.name)
  end

  def child_spec(opts) do
    %{id: {__MODULE__, Keyword.fetch!(opts, :name)}, start: {__MODULE__, :start_link, [opts]}}
  end

  def pause(server), do: GenServer.call(server, :pause)

  def await_idle(server, timeout_ms),
    do: GenServer.call(server, {:await_idle, timeout_ms}, timeout_ms + 1_000)

  def attached?(server), do: GenServer.call(server, :attached?)

  @impl true
  def init(config) do
    send(self(), :attach)

    {:ok,
     %{
       config: config,
       conn: nil,
       monitor: nil,
       retry: nil,
       sid: nil,
       inbox: nil,
       ref: nil,
       received: 0,
       ready: false,
       paused: false,
       tasks: %{},
       waiters: []
     }}
  end

  @impl true
  def handle_call(:pause, _from, state) do
    if state.ready, do: emit(state, :pause)
    {:reply, :ok, %{state | paused: true, ref: nil, ready: false}}
  end

  def handle_call(:attached?, _from, state), do: {:reply, state.ready, state}

  def handle_call({:await_idle, _timeout_ms}, _from, %{tasks: tasks} = state)
      when map_size(tasks) == 0,
      do: {:reply, :ok, state}

  def handle_call({:await_idle, timeout_ms}, from, state) do
    Process.send_after(self(), {:idle_timeout, from}, timeout_ms)
    {:noreply, %{state | waiters: [from | state.waiters]}}
  end

  @impl true
  def handle_info(:attach, %{paused: true} = state), do: {:noreply, %{state | retry: nil}}

  def handle_info(:attach, %{sid: sid} = state) when sid != nil,
    do: {:noreply, %{state | retry: nil}}

  def handle_info(:attach, state) do
    state = %{state | retry: nil}

    with %Planes{} = conn <- Planes.connection(state.config.plane),
         :ok <- consumer_exists(state.config, conn),
         inbox = inbox(state.config.inbox_prefix),
         {:ok, sid} <- Planes.subscribe(conn, self(), inbox) do
      monitor = monitor(state, conn)
      emit(state, :attach)

      {:noreply,
       pull(%{state | conn: conn, monitor: monitor, sid: sid, inbox: inbox, ready: true})}
    else
      _ ->
        {:noreply, schedule_attach(state)}
    end
  end

  def handle_info({:msg, %{sid: sid, status: status}}, %{sid: sid, paused: true} = state)
      when is_binary(status),
      do: {:noreply, drop_paused(state)}

  def handle_info({:msg, %{sid: sid} = message}, %{sid: sid, paused: true} = state) do
    nak_paused(message, state)
    {:noreply, state}
  end

  def handle_info({:msg, %{sid: sid} = message}, %{sid: sid} = state),
    do: {:noreply, receive_message(message, state)}

  def handle_info({:msg, %{reply_to: "$JS.ACK." <> _ = ack}}, state) do
    Ack.nak(state.config.plane, ack)
    {:noreply, state}
  end

  def handle_info({:guard, ref}, %{ref: ref} = state), do: {:noreply, pull(state)}

  def handle_info({ref, _result}, %{tasks: tasks} = state) when is_map_key(tasks, ref) do
    Process.demonitor(ref, [:flush])
    {:noreply, finish_task(ref, state)}
  end

  def handle_info({:DOWN, ref, :process, _pid, _reason}, %{tasks: tasks} = state)
      when is_map_key(tasks, ref),
      do: {:noreply, finish_task(ref, state)}

  def handle_info({:DOWN, _ref, :process, pid, _reason}, %{conn: %{pid: pid}} = state) do
    emit(state, :detach)
    state = %{state | conn: nil, monitor: nil, sid: nil, ref: nil, ready: false}
    {:noreply, schedule_attach(state)}
  end

  def handle_info({:idle_timeout, from}, state) do
    if from in state.waiters do
      GenServer.reply(from, {:error, :timeout})
      {:noreply, %{state | waiters: List.delete(state.waiters, from)}}
    else
      {:noreply, state}
    end
  end

  def handle_info(_message, state), do: {:noreply, state}

  defp drop_paused(state) do
    Planes.unsubscribe(state.conn, state.sid)
    %{state | sid: nil}
  end

  defp nak_paused(%{reply_to: "$JS.ACK." <> _ = ack}, state) do
    Ack.nak(state.config.plane, ack, @paused_nak_ns)
    emit(state, :paused_nak)
  end

  defp nak_paused(_message, _state), do: :ok

  defp receive_message(%{status: status}, state) when is_binary(status) do
    emit(state, :status, %{status: status})
    receive_status(status, state)
  end

  defp receive_message(message, state) do
    state = run(message, %{state | ready: true})
    received = state.received + 1
    if received >= state.config.batch, do: pull(state), else: %{state | received: received}
  end

  defp monitor(%{conn: %{pid: pid}, monitor: monitor}, %{pid: pid}), do: monitor

  defp monitor(state, conn) do
    if state.monitor, do: Process.demonitor(state.monitor, [:flush])
    Process.monitor(conn.pid)
  end

  defp consumer_exists(config, conn) do
    subject = "$JS.API.CONSUMER.INFO.#{config.stream}.#{config.durable}"

    with {:ok, %{body: body}} <-
           Planes.jetstream_request({subject, "", [receive_timeout: 2_000]}, conn),
         {:ok, %{"config" => _}} <- Jason.decode(body) do
      :ok
    end
  end

  defp receive_status("408", state), do: pull(%{state | ready: true})

  defp receive_status(_status, state) do
    Planes.unsubscribe(state.conn, state.sid)
    emit(state, :detach)
    schedule_attach(%{state | sid: nil, ref: nil, ready: false})
  end

  defp schedule_attach(%{retry: nil} = state),
    do: %{state | retry: Process.send_after(self(), :attach, @retry_ms)}

  defp schedule_attach(state), do: state

  defp emit(state, event, metadata \\ %{}) do
    :telemetry.execute(
      [:platform, :nats, :pull, event],
      %{count: 1},
      Map.put(metadata, :name, state.config.name)
    )
  end

  defp run(message, %{config: config} = state) do
    task =
      Task.Supervisor.async_nolink(config.task_supervisor, fn -> config.handler.(message) end)

    %{state | tasks: Map.put(state.tasks, task.ref, true)}
  end

  defp finish_task(ref, state) do
    state = %{state | tasks: Map.delete(state.tasks, ref)}
    if map_size(state.tasks) == 0, do: release(state), else: state
  end

  defp release(state) do
    Enum.each(state.waiters, &GenServer.reply(&1, :ok))
    %{state | waiters: []}
  end

  defp inbox(prefix),
    do: prefix <> "." <> Base.url_encode64(:crypto.strong_rand_bytes(18), padding: false)

  defp pull(%{config: config} = state) do
    ref = make_ref()
    subject = "$JS.API.CONSUMER.MSG.NEXT.#{config.stream}.#{config.durable}"
    body = Jason.encode!(%{batch: config.batch, expires: config.expires_ns})
    Nats.publish(state.conn.pid, subject, body, reply_to: state.inbox)
    Process.send_after(self(), {:guard, ref}, div(config.expires_ns, 1_000_000) + @guard_slack_ms)
    %{state | ref: ref, received: 0}
  end
end
