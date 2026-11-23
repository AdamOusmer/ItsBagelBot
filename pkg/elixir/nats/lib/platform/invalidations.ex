# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Invalidations do
  @moduledoc "Cache invalidation feed over core NATS, wire-compatible with the Go bus feed."
  use GenServer
  require Logger

  alias Platform.Nats
  alias Platform.Nats.Planes

  @origin_header "Bagel-Invalidation-Origin"
  @origin_header_received String.downcase(@origin_header)
  @retry_ms 1_000

  def start_link(opts) do
    Enum.each([:name, :plane, :prefix], &Keyword.fetch!(opts, &1))
    GenServer.start_link(__MODULE__, opts, name: Keyword.fetch!(opts, :name))
  end

  def child_spec(opts) do
    %{id: {__MODULE__, Keyword.fetch!(opts, :name)}, start: {__MODULE__, :start_link, [opts]}}
  end

  def watch(feed, scope, fun, owner \\ nil) when is_binary(scope) and is_function(fun, 1),
    do: GenServer.call(feed, {:watch, scope, fun, owner || make_ref()})

  def watch_gap(feed, fun, owner \\ nil) when is_function(fun, 0),
    do: GenServer.call(feed, {:watch_gap, fun, owner || make_ref()})

  def publish(feed, scope, id, keys \\ []) when is_binary(scope) and is_list(keys),
    do: GenServer.call(feed, {:publish, scope, id, keys})

  @impl true
  def init(opts) do
    send(self(), :attach)

    {:ok,
     %{
       plane: Keyword.fetch!(opts, :plane),
       prefix: Keyword.fetch!(opts, :prefix),
       retry_ms: Keyword.get(opts, :retry_ms, @retry_ms),
       origin: Base.encode16(:crypto.strong_rand_bytes(8)),
       watchers: %{},
       gaps: [],
       conn: nil,
       retrying: true,
       subscribed: MapSet.new()
     }}
  end

  @impl true
  def handle_call({:watch, scope, fun, owner}, _from, state) do
    watchers =
      Map.update(
        state.watchers,
        scope,
        [{owner, fun}],
        &List.keystore(&1, owner, 0, {owner, fun})
      )

    {:reply, :ok, resubscribe(%{state | watchers: watchers})}
  end

  def handle_call({:watch_gap, fun, owner}, _from, state),
    do: {:reply, :ok, %{state | gaps: List.keystore(state.gaps, owner, 0, {owner, fun})}}

  def handle_call({:publish, scope, id, keys}, _from, state) do
    dispatch(state, %{scope: scope, id: id, keys: keys})
    {:reply, send_event(state, scope, id, keys), state}
  end

  @impl true
  def handle_info(:attach, %{conn: nil} = state) do
    state = %{state | retrying: false}

    case connection_pid(state.plane) do
      nil ->
        {:noreply, retry(state)}

      pid ->
        Process.monitor(pid)
        {:noreply, attach(%{state | conn: pid, subscribed: MapSet.new()})}
    end
  end

  def handle_info(:attach, state), do: {:noreply, attach(%{state | retrying: false})}

  def handle_info({:DOWN, _ref, :process, pid, _reason}, %{conn: pid} = state),
    do: {:noreply, retry(%{state | conn: nil, subscribed: MapSet.new()})}

  def handle_info({:msg, message}, state) do
    receive_message(state, message)
    {:noreply, state}
  end

  def handle_info(_message, state), do: {:noreply, state}

  defp connection_pid(plane) when plane in [:rpc, :jetstream] do
    case Planes.connection(plane) do
      %Planes{pid: pid} -> pid
      nil -> nil
    end
  end

  defp connection_pid(connection), do: Nats.connection_pid(connection)

  defp attach(state) do
    state = subscribe_all(state)

    if subscribed_all?(state) do
      Enum.each(state.gaps, fn {_owner, fun} -> fun.() end)
      state
    else
      retry(state)
    end
  end

  defp resubscribe(state) do
    state = subscribe_all(state)
    if subscribed_all?(state), do: state, else: retry(state)
  end

  defp retry(%{retrying: true} = state), do: state

  defp retry(state) do
    Process.send_after(self(), :attach, state.retry_ms)
    %{state | retrying: true}
  end

  defp wire_scopes(state),
    do: for({scope, _funs} <- state.watchers, not wire_excluded?(scope), do: scope)

  defp subscribed_all?(%{conn: nil}), do: false

  defp subscribed_all?(state),
    do: Enum.all?(wire_scopes(state), &MapSet.member?(state.subscribed, &1))

  defp subscribe_all(%{conn: nil} = state), do: state
  defp subscribe_all(state), do: Enum.reduce(wire_scopes(state), state, &subscribe_scope/2)

  defp subscribe_scope(scope, state) do
    if MapSet.member?(state.subscribed, scope),
      do: state,
      else: subscribe_new(scope, state)
  end

  defp subscribe_new(scope, state) do
    case Nats.subscribe(state.conn, self(), subject(state, scope)) do
      {:ok, _sid} ->
        %{state | subscribed: MapSet.put(state.subscribed, scope)}

      {:error, reason} ->
        Logger.warning("cache invalidation subscribe failed: #{inspect({scope, reason})}")
        state
    end
  end

  defp wire_excluded?(scope), do: String.contains?(scope, ".")

  defp subject(state, scope), do: state.prefix <> "." <> scope

  defp receive_message(state, %{topic: topic, body: body} = message) do
    with false <- own_origin?(state, message),
         {:ok, %{"broadcaster_id" => raw_id} = dto} <- Jason.decode(body),
         {:ok, id} <- parse_id(raw_id) do
      dispatch(state, %{scope: scope_of(topic), id: id, keys: keys_of(dto)})
    else
      _ -> :ok
    end
  end

  defp own_origin?(state, message) do
    message |> Map.get(:headers, []) |> List.keyfind(@origin_header_received, 0) ==
      {@origin_header_received, state.origin}
  end

  defp scope_of(topic), do: topic |> String.split(".") |> List.last()

  defp parse_id("*"), do: {:ok, :all}

  defp parse_id(raw) when is_binary(raw) do
    case Integer.parse(raw) do
      {id, ""} when id > 0 -> {:ok, id}
      _ -> :error
    end
  end

  defp parse_id(_raw), do: :error

  defp keys_of(%{"keys" => keys}) when is_list(keys), do: Enum.filter(keys, &is_binary/1)
  defp keys_of(_dto), do: []

  defp dispatch(state, %{scope: scope} = event) do
    state.watchers |> Map.get(scope, []) |> Enum.each(fn {_owner, fun} -> fun.(event) end)
  end

  defp send_event(%{conn: nil}, _scope, _id, _keys), do: {:error, :not_connected}

  defp send_event(state, scope, id, keys) do
    body = Jason.encode!(%{broadcaster_id: wire_id(id), keys: keys})

    Nats.publish(state.conn, subject(state, scope), body,
      headers: [{@origin_header, state.origin}]
    )
  end

  defp wire_id(:all), do: "*"
  defp wire_id(id) when is_integer(id), do: Integer.to_string(id)
end
