# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.Requester do
  @moduledoc "Request-reply over one wildcard reply subscription under a caller-chosen prefix."
  use GenServer

  alias Platform.Nats
  alias Platform.Nats.Planes

  @retry_ms 1_000

  def start_link(opts) do
    config = %{
      plane: Keyword.get(opts, :plane, :jetstream),
      prefix: Keyword.fetch!(opts, :prefix)
    }

    GenServer.start_link(__MODULE__, config, name: Keyword.fetch!(opts, :name))
  end

  def child_spec(opts) do
    %{id: {__MODULE__, Keyword.fetch!(opts, :name)}, start: {__MODULE__, :start_link, [opts]}}
  end

  def request(name, subject, body, timeout_ms) when is_integer(timeout_ms) and timeout_ms > 0 do
    headers = Platform.NewRelic.trace_headers()
    GenServer.call(name, {:request, subject, body, headers, timeout_ms}, timeout_ms + 1_000)
  catch
    :exit, {:noproc, _} -> {:error, :not_connected}
  end

  @impl true
  def init(config) do
    send(self(), :attach)

    {:ok,
     Map.merge(config, %{conn: nil, sid: nil, waiting: %{}, skip: byte_size(config.prefix) + 1})}
  end

  @impl true
  def handle_call(
        {:request, _subject, _body, _headers, _timeout_ms},
        _from,
        %{conn: nil} = state
      ),
      do: {:reply, {:error, :not_connected}, state}

  def handle_call({:request, subject, body, headers, timeout_ms}, from, state) do
    nonce = Base.url_encode64(:crypto.strong_rand_bytes(12), padding: false)
    opts = publish_opts(state.prefix <> "." <> nonce, headers)

    case Nats.publish(state.conn.pid, subject, body, opts) do
      :ok ->
        timer = Process.send_after(self(), {:expired, nonce}, timeout_ms)
        {:noreply, %{state | waiting: Map.put(state.waiting, nonce, {from, timer})}}

      {:error, _reason} ->
        {:reply, {:error, :not_connected}, state}
    end
  end

  @impl true
  def handle_info(:attach, state) do
    with %Planes{} = conn <- Planes.connection(state.plane),
         {:ok, sid} <- Planes.subscribe(conn, self(), state.prefix <> ".*") do
      Process.monitor(conn.pid)
      {:noreply, %{state | conn: conn, sid: sid}}
    else
      _ ->
        Process.send_after(self(), :attach, @retry_ms)
        {:noreply, state}
    end
  end

  def handle_info({:msg, %{sid: sid} = message}, %{sid: sid} = state),
    do: {:noreply, settle(state, nonce(message.topic, state.skip), answer(message))}

  def handle_info({:expired, nonce}, state),
    do: {:noreply, settle(state, nonce, {:error, :timeout})}

  def handle_info({:DOWN, _ref, :process, pid, _reason}, %{conn: %{pid: pid}} = state) do
    Process.send_after(self(), :attach, @retry_ms)

    for {_nonce, {from, timer}} <- state.waiting do
      Process.cancel_timer(timer)
      GenServer.reply(from, {:error, :not_connected})
    end

    {:noreply, %{state | conn: nil, sid: nil, waiting: %{}}}
  end

  def handle_info(_message, state), do: {:noreply, state}

  defp publish_opts(reply, []), do: [reply_to: reply]
  defp publish_opts(reply, headers), do: [reply_to: reply, headers: headers]

  defp nonce(topic, skip) do
    binary_part(topic, skip, byte_size(topic) - skip)
  end

  defp answer(%{status: "503"}), do: {:error, :no_responders}
  defp answer(%{body: body}), do: {:ok, body}

  defp settle(state, nonce, result) do
    case Map.pop(state.waiting, nonce) do
      {nil, _waiting} ->
        state

      {{from, timer}, waiting} ->
        Process.cancel_timer(timer)
        GenServer.reply(from, result)
        %{state | waiting: waiting}
    end
  end
end
