# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.BroadcasterCache do
  use GenServer
  require Logger

  @default_table __MODULE__
  @error_ttl_ms 5_000
  @sweep_interval_ms 60_000

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  @spec lane(String.t(), GenServer.server()) :: :premium | :standard | :drop
  def lane(broadcaster_id, server \\ __MODULE__) do
    table = table_name(server)
    now = now_ms()

    case :ets.lookup(table, broadcaster_id) do
      [{^broadcaster_id, lane, expires_at, _refreshing}] when expires_at > now ->
        lane

      [{^broadcaster_id, lane, _expires_at, refreshing} = row] ->
        refresh_stale(refreshing, row, table, server)
        lane

      [] ->
        GenServer.call(server, {:lookup, broadcaster_id})
    end
  end

  defp refresh_stale(false, row, table, server) do
    if claim_refresh(row, table), do: GenServer.cast(server, {:refresh, elem(row, 0)})
  end

  defp refresh_stale(true, _row, _table, _server), do: :ok

  defp claim_refresh(row, table) do
    :ets.select_replace(table, [{row, [], [{:const, put_elem(row, 3, true)}]}]) == 1
  end

  @spec invalidate(String.t(), GenServer.server()) :: :ok
  def invalidate(broadcaster_id, server \\ __MODULE__) do
    GenServer.call(server, {:invalidate, broadcaster_id})
  end

  @spec invalidate_all(GenServer.server()) :: :ok
  def invalidate_all(server \\ __MODULE__) do
    GenServer.call(server, :invalidate_all)
  end

  @impl true
  def init(opts) do
    table = Keyword.get(opts, :table, @default_table)
    :ets.new(table, [:set, :public, :named_table, read_concurrency: true])
    sweep_interval_ms = Keyword.get(opts, :sweep_interval_ms, @sweep_interval_ms)
    Process.send_after(self(), :sweep, sweep_interval_ms)

    {:ok,
     %{
       table: table,
       ttl_ms: Keyword.get(opts, :ttl_ms) || Ingress.Config.broadcaster_cache_ttl_ms(),
       sweep_interval_ms: sweep_interval_ms,
       loader: Keyword.get(opts, :loader, &Ingress.BroadcasterStatus.lane_for/1),
       pending_by_id: %{},
       id_by_ref: %{},
       ref_by_id: %{},
       stale_refs: MapSet.new()
     }}
  end

  @impl true
  def handle_call({:lookup, id}, from, state) do
    case :ets.lookup(state.table, id) do
      [{^id, lane, _expires_at, _refreshing}] -> {:reply, lane, state}
      [] -> {:noreply, await_load(id, from, state)}
    end
  end

  def handle_call({:invalidate, id}, _from, state) do
    :ets.delete(state.table, id)
    {:reply, :ok, reload_pending(id, state)}
  end

  def handle_call(:invalidate_all, _from, state) do
    :ets.delete_all_objects(state.table)

    state =
      Enum.reduce(state.pending_by_id, state, fn {id, _waiters}, acc_state ->
        reload_pending(id, acc_state)
      end)

    {:reply, :ok, state}
  end

  defp reload_pending(id, state) do
    case Map.fetch(state.ref_by_id, id) do
      :error ->
        state

      {:ok, ref} ->
        start_load(%{state | stale_refs: MapSet.put(state.stale_refs, ref)}, id)
    end
  end

  @impl true
  def handle_cast({:refresh, id}, state) do
    if Map.has_key?(state.pending_by_id, id) do
      {:noreply, state}
    else
      Ingress.Metrics.count("Cache/Refreshes")
      {:noreply, start_load(%{state | pending_by_id: Map.put(state.pending_by_id, id, [])}, id)}
    end
  end

  defp start_load(state, id) do
    task =
      Task.Supervisor.async_nolink(Ingress.BroadcasterCache.TaskSupervisor, fn ->
        state.loader.(id)
      end)

    Ingress.Metrics.count("Cache/Loads")
    track_task(state, id, task.ref)
  end

  defp track_task(state, id, ref) do
    %{
      state
      | id_by_ref: Map.put(state.id_by_ref, ref, id),
        ref_by_id: Map.put(state.ref_by_id, id, ref)
    }
  end

  defp untrack_ref(ref_by_id, id, ref) do
    case ref_by_id do
      %{^id => ^ref} -> Map.delete(ref_by_id, id)
      _ -> ref_by_id
    end
  end

  @impl true
  def handle_info({ref, result}, state) when is_reference(ref) do
    Process.demonitor(ref, [:flush])
    handle_task_result(ref, result, state)
  end

  def handle_info({:DOWN, ref, :process, _pid, reason}, state) do
    if reason != :normal do
      Logger.warning("broadcaster status loader task failed: #{inspect(reason)}")
      Ingress.Metrics.count("Cache/TaskFailed")
    end

    handle_task_result(ref, {:error, reason}, state)
  end

  def handle_info(:sweep, state) do
    idle_before = now_ms() - state.ttl_ms

    :ets.select_delete(state.table, [{{:_, :_, :"$1", :_}, [{:"=<", :"$1", idle_before}], [true]}])

    Process.send_after(self(), :sweep, state.sweep_interval_ms)
    {:noreply, state}
  end

  defp await_load(id, from, state) do
    case Map.fetch(state.pending_by_id, id) do
      {:ok, waiters} ->
        Ingress.Metrics.count("Cache/LoadsCoalesced")
        %{state | pending_by_id: Map.put(state.pending_by_id, id, [from | waiters])}

      :error ->
        start_load(%{state | pending_by_id: Map.put(state.pending_by_id, id, [from])}, id)
    end
  end

  defp handle_task_result(ref, result, state) do
    case Map.pop(state.id_by_ref, ref) do
      {nil, _} ->
        {:noreply, state}

      {id, id_by_ref} ->
        state = %{
          state
          | id_by_ref: id_by_ref,
            ref_by_id: untrack_ref(state.ref_by_id, id, ref)
        }

        if MapSet.member?(state.stale_refs, ref) do
          Ingress.Metrics.count("Cache/StaleIgnored")
          state = %{state | stale_refs: MapSet.delete(state.stale_refs, ref)}
          {:noreply, state}
        else
          lane = cache_result(id, result, state)
          {waiters, pending_by_id} = Map.pop(state.pending_by_id, id)
          Enum.each(waiters || [], fn from -> GenServer.reply(from, lane) end)
          {:noreply, %{state | pending_by_id: pending_by_id}}
        end
    end
  end

  defp cache_result(id, {:ok, lane}, state) when lane in [:premium, :standard, :drop] do
    :ets.insert(state.table, {id, lane, now_ms() + state.ttl_ms, false})
    lane
  end

  defp cache_result(id, {:error, reason}, state) do
    Logger.warning("broadcaster status lookup failed for #{id}: #{inspect(reason)}")
    Ingress.Metrics.count("Cache/LoadErrors")
    lane = last_known_lane(state.table, id)
    :ets.insert(state.table, {id, lane, now_ms() + @error_ttl_ms, false})
    lane
  end

  defp last_known_lane(table, id) do
    case :ets.lookup(table, id) do
      [{^id, lane, _expires_at, _refreshing}] -> lane
      [] -> :standard
    end
  end

  defp table_name(__MODULE__), do: @default_table
  defp table_name(server) when is_atom(server), do: server
  defp table_name(_), do: @default_table

  defp now_ms, do: System.monotonic_time(:millisecond)
end
