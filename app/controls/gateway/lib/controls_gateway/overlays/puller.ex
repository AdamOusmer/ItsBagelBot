# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlays.Puller do
  use GenServer

  alias ControlsGateway.Overlays.{Delivery, Tiers}
  alias Platform.Nats
  alias Platform.Nats.Planes

  @batch 16
  @expires_ns 25_000_000_000
  @guard_ms 30_000
  @retry_ms 1_000

  def start_link(tier), do: GenServer.start_link(__MODULE__, tier, name: name(tier))

  def child_spec(tier) do
    %{id: {__MODULE__, tier}, start: {__MODULE__, :start_link, [tier]}}
  end

  def pause_all do
    for tier <- Tiers.all(), pid = Process.whereis(name(tier)), do: GenServer.call(pid, :pause)
    :ok
  end

  defp name(tier), do: Module.concat(__MODULE__, tier |> Atom.to_string() |> Macro.camelize())

  @impl true
  def init(tier) do
    send(self(), :attach)
    {:ok, %{tier: tier, conn: nil, sid: nil, inbox: nil, ref: nil, received: 0, paused: false}}
  end

  @impl true
  def handle_call(:pause, _from, state) do
    if state.sid, do: Planes.unsubscribe(state.conn, state.sid)
    {:reply, :ok, %{state | paused: true, sid: nil, ref: nil}}
  end

  @impl true
  def handle_info(:attach, %{paused: true} = state), do: {:noreply, state}

  def handle_info(:attach, state) do
    with %Planes{} = conn <- Planes.connection(:jetstream),
         inbox = inbox(),
         {:ok, sid} <- Planes.subscribe(conn, self(), inbox) do
      Process.monitor(conn.pid)
      {:noreply, pull(%{state | conn: conn, sid: sid, inbox: inbox})}
    else
      _ ->
        Process.send_after(self(), :attach, @retry_ms)
        {:noreply, state}
    end
  end

  def handle_info({:msg, %{sid: sid} = message}, %{sid: sid} = state),
    do: {:noreply, receive_message(message, state)}

  def handle_info({:guard, ref}, %{ref: ref} = state), do: {:noreply, pull(state)}

  def handle_info({:DOWN, _ref, :process, pid, _reason}, %{conn: %{pid: pid}} = state) do
    Process.send_after(self(), :attach, @retry_ms)
    {:noreply, %{state | conn: nil, sid: nil, ref: nil}}
  end

  def handle_info(_message, state), do: {:noreply, state}

  defp receive_message(%{status: "408"}, state), do: pull(state)

  defp receive_message(%{status: status}, state) when is_binary(status) do
    Process.send_after(self(), {:guard, state.ref}, @retry_ms)
    state
  end

  defp receive_message(message, state) do
    Task.start(fn -> Delivery.handle(message) end)
    received = state.received + 1
    if received >= @batch, do: pull(state), else: %{state | received: received}
  end

  defp inbox do
    "_INBOX.overlays." <> Base.url_encode64(:crypto.strong_rand_bytes(18), padding: false)
  end

  defp pull(state) do
    ref = make_ref()
    stream = Tiers.stream(state.tier)
    subject = "$JS.API.CONSUMER.MSG.NEXT.#{stream}.#{Tiers.durable(state.tier)}"
    body = Jason.encode!(%{batch: @batch, expires: @expires_ns})
    Nats.publish(state.conn.pid, subject, body, reply_to: state.inbox)
    Process.send_after(self(), {:guard, ref}, @guard_ms)
    %{state | ref: ref, received: 0}
  end
end
