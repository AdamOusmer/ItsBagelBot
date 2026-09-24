# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ConduitManager do
  use GenServer
  require Logger

  alias Ingress.Metrics
  alias Ingress.ShardDistribution
  alias Ingress.ShardHealth
  alias Ingress.ShardInventory
  alias Ingress.ShardScaler
  alias Ingress.ShardSession
  alias Ingress.Singleton
  alias Ingress.Twitch.Api

  @reconcile_interval_ms 15_000
  @retry_interval_ms 5_000
  @orphan_stop_timeout_ms 5_000
  @rebalance_stable_ticks 9
  @rebalance_handoff_timeout_ms 20_000
  @rebalance_poll_interval_ms 300
  @rebalance_name_release_timeout_ms 5_000

  def start_link(_opts) do
    GenServer.start_link(__MODULE__, [], name: via())
  end

  def via, do: {:via, Horde.Registry, {Ingress.Registry, :conduit_manager}}

  @impl true
  def init(_) do
    {:ok,
     %{
       conduit_id: nil,
       applied_shard_count: nil,
       adopted_scaler: nil,
       unhealthy_counts: %{},
       rescues: %{},
       placement_nodes: [],
       placement_stable_ticks: 0,
       standby?: false
     }, {:continue, :reconcile}}
  end

  @impl true
  def handle_continue(:reconcile, state), do: {:noreply, reconcile(state)}

  @impl true
  def handle_call(:status, _from, state) do
    {:reply, %{node: node(), conduit_id: state.conduit_id}, state}
  end

  @impl true
  def handle_info(:reconcile, state), do: {:noreply, reconcile(state)}

  defp reconcile(state) do
    case singleton_status(state) do
      {:standby, state} ->
        Process.send_after(self(), :reconcile, @reconcile_interval_ms)
        state

      {:active, state} ->
        converge(state)
    end
  end

  defp singleton_status(state) do
    case Singleton.lookup(:conduit_manager) do
      {:ok, pid} when pid != self() -> {:standby, enter_standby(state)}
      _ours_or_lagging -> {:active, leave_standby(state)}
    end
  end

  defp enter_standby(%{standby?: true} = state), do: state

  defp enter_standby(state) do
    Logger.warning("conduit manager no longer holds the singleton name; standing by")
    Metrics.count("Conduit/ManagerStandby")
    %{state | standby?: true}
  end

  defp leave_standby(%{standby?: false} = state), do: state

  defp leave_standby(state) do
    Logger.info("conduit manager holds the singleton name again; resuming reconcile")
    %{state | standby?: false}
  end

  defp converge(state) do
    case ensure_conduit(state) do
      {:ok, conduit_id, applied_shard_count} ->
        state = %{state | conduit_id: conduit_id, applied_shard_count: applied_shard_count}
        converge_with_scaler(state)

      {:error, reason} ->
        Logger.error("conduit reconcile failed: #{inspect(reason)}")
        Process.send_after(self(), :reconcile, @retry_interval_ms)
        state
    end
  end

  defp ensure_conduit(%{conduit_id: nil}), do: Api.ensure_conduit()
  defp ensure_conduit(%{conduit_id: id, applied_shard_count: count}), do: {:ok, id, count}

  # Live scaler answers only: shrinking to its config-floor fallback drops autoscaled bindings.
  defp converge_with_scaler(state) do
    case ShardScaler.fetch_desired() do
      {:ok, _desired, scaler} when scaler != state.adopted_scaler ->
        adopt_then_converge(state, scaler)

      {:ok, desired, _scaler} ->
        snapshot = shard_snapshot(state.conduit_id)
        applied = converge_shards(state.conduit_id, desired, state.applied_shard_count, snapshot)
        state = run_health_pass(%{state | applied_shard_count: applied}, desired, snapshot)
        state = rebalance_shards(state, desired, snapshot)
        Process.send_after(self(), :reconcile, @reconcile_interval_ms)
        state

      :error ->
        hold_convergence(state, "shard scaler unreachable")
    end
  end

  defp adopt_then_converge(state, scaler) do
    case adopt_applied_count(state.applied_shard_count) do
      :ok -> converge_with_scaler(%{state | adopted_scaler: scaler})
      :error -> hold_convergence(state, "shard target adoption failed")
    end
  end

  defp hold_convergence(state, reason) do
    Logger.warning("#{reason}; holding shard convergence")
    Process.send_after(self(), :reconcile, @retry_interval_ms)
    state
  end

  defp adopt_applied_count(applied) do
    case ShardScaler.fetch_desired() do
      {:ok, desired, _scaler} when applied > desired ->
        case ShardScaler.set_target(applied) do
          :ok -> :ok
          {:error, _} -> :error
        end

      {:ok, _desired, _scaler} ->
        :ok

      :error ->
        :error
    end
  end

  defp shard_snapshot(conduit_id) do
    case Api.get_shards(conduit_id) do
      {:ok, shards} ->
        {:ok, shards}

      {:error, reason} ->
        Logger.warning("shard snapshot failed: #{inspect(reason)}")
        :error
    end
  end

  defp converge_shards(conduit_id, desired, applied, snapshot) when desired > 0 do
    applied = maybe_resize_conduit(conduit_id, desired, applied)

    stop_excess_shards(desired)
    sweep_unmanaged_shards(desired)
    start_missing_shards(conduit_id, desired, snapshot)
    applied
  end

  defp maybe_resize_conduit(_conduit_id, desired, desired), do: desired

  defp maybe_resize_conduit(conduit_id, desired, applied) do
    case Api.update_conduit(conduit_id, desired) do
      :ok ->
        Logger.info("conduit resized to #{desired} shards")
        desired

      {:error, reason} ->
        Logger.warning("conduit resize to #{desired} failed: #{inspect(reason)}")
        applied
    end
  end

  defp stop_excess_shards(desired) do
    running_ids =
      Horde.Registry.select(Ingress.Registry, [
        {{{:shard, :"$1"}, :_, :_}, [], [:"$1"]}
      ])

    for shard_id <- running_ids, shard_id >= desired do
      case Horde.Registry.lookup(Ingress.Registry, {:shard, shard_id}) do
        [{pid, _}] ->
          Logger.info("stopping excess shard #{shard_id} (desired=#{desired})")
          terminate_shard(shard_id, pid)

        [] ->
          :ok
      end
    end
  end

  defp sweep_unmanaged_shards(desired) do
    for {pid, status} <- ShardInventory.unmanaged(),
        ShardHealth.unmanaged_action(status, desired) == :stop do
      Logger.warning("stopping unmanaged shard #{status.shard_id} (supervised, unregistered)")
      Metrics.count("Conduit/UnmanagedShardStops")
      terminate_shard(status.shard_id, pid)
    end
  end

  defp terminate_shard(shard_id, pid) do
    case Horde.DynamicSupervisor.terminate_child(Ingress.ShardSupervisor, pid) do
      :ok -> :ok
      {:error, :not_found} -> stop_orphan_shard(shard_id, pid)
      {:error, reason} -> Logger.warning("stop shard #{shard_id} failed: #{inspect(reason)}")
    end
  end

  defp stop_orphan_shard(shard_id, pid) do
    Logger.warning("stopping orphan shard #{shard_id} (registered but unsupervised)")

    try do
      GenServer.stop(pid, :normal, @orphan_stop_timeout_ms)
    catch
      :exit, reason ->
        Logger.warning("orphan shard #{shard_id} stop failed: #{inspect(reason)}")
    end
  end

  defp start_missing_shards(conduit_id, desired, snapshot) do
    for shard_id <- startable_ids(snapshot, desired), do: start_shard(conduit_id, shard_id)
  end

  defp startable_ids({:ok, shards}, desired), do: ShardHealth.startable_ids(shards, desired)
  defp startable_ids(:error, desired), do: Enum.to_list(0..(desired - 1))

  defp start_shard(conduit_id, shard_id) do
    spec = {Ingress.ShardSession, shard_id: shard_id, conduit_id: conduit_id}

    case Horde.DynamicSupervisor.start_child(Ingress.ShardSupervisor, spec) do
      {:ok, pid} ->
        Logger.info("started shard #{shard_id}")
        {:started, pid}

      {:error, {:already_started, _pid}} ->
        :blocked

      :ignore ->
        :blocked

      {:error, reason} ->
        Logger.warning("shard #{shard_id} start failed: #{inspect(reason)}")
        :blocked
    end
  end

  defp rebalance_shards(state, desired, snapshot) do
    nodes = placement_nodes()
    state = track_placement_membership(state, nodes)

    if rebalance_ready?(state, desired, snapshot, nodes) do
      case rebalance_candidate(desired, nodes) do
        nil ->
          state

        {shard_id, pid, target} ->
          rebalance_shard(state.conduit_id, shard_id, pid, target)
          state
      end
    else
      state
    end
  end

  defp placement_nodes do
    visible = MapSet.new([node() | Node.list()])

    Horde.Cluster.members(Ingress.ShardSupervisor)
    |> Enum.map(fn
      {_supervisor, member_node} -> member_node
      member_node when is_atom(member_node) -> member_node
    end)
    |> Enum.filter(&MapSet.member?(visible, &1))
    |> Enum.reject(&ShardDistribution.draining_node?/1)
    |> Enum.uniq()
    |> Enum.sort()
  catch
    :exit, _ -> []
  end

  defp track_placement_membership(%{placement_nodes: nodes} = state, nodes) do
    %{state | placement_stable_ticks: state.placement_stable_ticks + 1}
  end

  defp track_placement_membership(state, nodes) do
    %{state | placement_nodes: nodes, placement_stable_ticks: 1}
  end

  defp rebalance_ready?(state, desired, {:ok, shards}, nodes) do
    state.placement_stable_ticks >= @rebalance_stable_ticks and
      length(nodes) > 1 and
      desired >= length(nodes) and
      state.rescues == %{} and
      ShardHealth.unhealthy_ids(shards, desired) == []
  end

  defp rebalance_ready?(_state, _desired, _snapshot, _nodes), do: false

  defp rebalance_candidate(desired, nodes) do
    placements =
      0..(desired - 1)
      |> Enum.reduce_while([], fn shard_id, acc ->
        case Horde.Registry.lookup(Ingress.Registry, {:shard, shard_id}) do
          [{pid, _}] -> {:cont, [{shard_id, pid} | acc]}
          [] -> {:halt, :incomplete}
        end
      end)

    case placements do
      :incomplete -> nil
      complete -> ShardDistribution.rebalance_candidate(complete, nodes)
    end
  end

  defp rebalance_shard(conduit_id, shard_id, pid, target) do
    Logger.info("rebalancing shard #{shard_id} #{node(pid)} → #{target} after stable membership")

    with :ok <- release_shard_name(pid),
         {:started, successor} <- start_successor(conduit_id, shard_id),
         :ok <- await_bound(successor, rebalance_deadline()) do
      GenServer.stop(pid, :normal, @orphan_stop_timeout_ms)
      Metrics.count("Conduit/ShardRebalances")
      :ok
    else
      failure ->
        Logger.warning("shard #{shard_id} rebalance failed: #{inspect(failure)}; rolling back")
        rollback_rebalance(shard_id, pid)
    end
  catch
    :exit, reason ->
      Logger.warning("shard #{shard_id} rebalance exited: #{inspect(reason)}; rolling back")
      rollback_rebalance(shard_id, pid)
  end

  defp release_shard_name(pid) do
    GenServer.call(pid, :release_name, @orphan_stop_timeout_ms)
  catch
    :exit, reason -> {:error, {:release_failed, reason}}
  end

  defp start_successor(conduit_id, shard_id) do
    start_until_free(
      fn -> start_shard(conduit_id, shard_id) end,
      name_release_deadline()
    )
  end

  @doc false
  def start_until_free(start_fun, deadline, opts \\ []) do
    timing = %{
      poll_ms: Keyword.get(opts, :poll_ms, @rebalance_poll_interval_ms),
      now: Keyword.get(opts, :now_fun, &monotonic_ms/0),
      sleep: Keyword.get(opts, :sleep_fun, &Process.sleep/1)
    }

    retry_until_free(start_fun, deadline, timing)
  end

  defp retry_until_free(start_fun, deadline, timing) do
    case start_fun.() do
      {:started, pid} -> {:started, pid}
      :blocked -> retry_blocked_start(start_fun, deadline, timing)
    end
  end

  defp retry_blocked_start(start_fun, deadline, timing) do
    if timing.now.() >= deadline do
      {:error, :name_release_timeout}
    else
      timing.sleep.(timing.poll_ms)
      retry_until_free(start_fun, deadline, timing)
    end
  end

  defp monotonic_ms, do: System.monotonic_time(:millisecond)

  defp name_release_deadline,
    do: monotonic_ms() + @rebalance_name_release_timeout_ms

  defp rebalance_deadline,
    do: monotonic_ms() + @rebalance_handoff_timeout_ms

  defp await_bound(pid, deadline) do
    cond do
      match?(%{bound: true}, probe_shard(pid)) ->
        :ok

      monotonic_ms() >= deadline ->
        {:error, :bind_timeout}

      true ->
        Process.sleep(@rebalance_poll_interval_ms)
        await_bound(pid, deadline)
    end
  end

  defp rollback_rebalance(shard_id, old_pid) do
    case Horde.Registry.lookup(Ingress.Registry, {:shard, shard_id}) do
      [{successor, _}] when successor != old_pid ->
        case Horde.DynamicSupervisor.terminate_child(Ingress.ShardSupervisor, successor) do
          :ok -> :ok
          {:error, :not_found} -> GenServer.stop(successor, :normal, @orphan_stop_timeout_ms)
          {:error, _reason} -> :ok
        end

      _ ->
        :ok
    end

    reclaim_old_shard(old_pid, 5)
    GenServer.cast(old_pid, :reassert_binding)
  catch
    :exit, _ -> :ok
  end

  defp reclaim_old_shard(_pid, 0), do: :error

  defp reclaim_old_shard(pid, attempts) do
    case GenServer.call(pid, :reclaim_name, @orphan_stop_timeout_ms) do
      {:ok, _owner} ->
        :ok

      {:error, {:already_registered, ^pid}} ->
        :ok

      {:error, {:already_registered, _other}} ->
        Process.sleep(@rebalance_poll_interval_ms)
        reclaim_old_shard(pid, attempts - 1)
    end
  catch
    :exit, _ -> :error
  end

  defp run_health_pass(state, desired, {:ok, shards}) do
    unhealthy = ShardHealth.unhealthy_ids(shards, desired)
    {counts, rescues} = heal_all(state, unhealthy)
    rescues = reap_rescues(unhealthy, rescues)
    %{state | unhealthy_counts: counts, rescues: rescues}
  end

  defp run_health_pass(state, _desired, :error), do: state

  defp heal_all(state, unhealthy) do
    Enum.reduce(unhealthy, {%{}, state.rescues}, fn shard_id, {counts, rescues} ->
      seen = Map.get(state.unhealthy_counts, shard_id, 0) + 1
      {count, rescues} = heal_shard({state.conduit_id, shard_id}, seen, rescues)
      {Map.put(counts, shard_id, count), rescues}
    end)
  end

  defp heal_shard({_conduit_id, shard_id} = slot, seen, rescues) do
    case Horde.Registry.lookup(Ingress.Registry, {:shard, shard_id}) do
      [] -> heal_unregistered(slot, seen, rescues)
      [{pid, _}] -> heal_registered(slot, pid, seen, rescues)
    end
  end

  defp heal_unregistered(slot, seen, rescues) do
    if ShardHealth.escalate?(seen) do
      ensure_rescue(slot, seen, rescues)
    else
      {seen, rescues}
    end
  end

  defp heal_registered({_conduit_id, shard_id} = slot, pid, seen, rescues) do
    case ShardHealth.heal_action(probe_shard(pid), seen) do
      :skip ->
        {seen, rescues}

      :force_rebind ->
        Logger.warning("shard #{shard_id} unhealthy on Twitch but bound locally; forcing re-bind")
        Metrics.count("Conduit/ShardRebinds")
        GenServer.cast(pid, :force_rebind)
        {seen, rescues}

      :restart ->
        restart_registered(slot, pid, seen, rescues)
    end
  end

  defp restart_registered({_conduit_id, shard_id} = slot, pid, seen, rescues) do
    Logger.warning("shard #{shard_id} unhealthy on Twitch (#{seen} ticks); replacing session")
    Metrics.count("Conduit/ShardRestarts")

    case restart_shard(slot, pid) do
      {:started, _pid} -> {0, rescues}
      :blocked -> ensure_rescue(slot, seen, rescues)
    end
  end

  defp restart_shard({conduit_id, shard_id}, pid) do
    terminate_shard(shard_id, pid)
    start_shard(conduit_id, shard_id)
  end

  defp probe_shard(pid) do
    ShardSession.status(pid)
  catch
    :exit, _ -> :unreachable
  end

  defp ensure_rescue({_conduit_id, shard_id} = slot, seen, rescues) do
    case Map.get(rescues, shard_id) do
      nil -> {seen, spawn_rescue(slot, seen, rescues)}
      entry -> heal_rescue(slot, entry, seen, rescues)
    end
  end

  defp spawn_rescue({conduit_id, shard_id}, seen, rescues) do
    Logger.warning("shard #{shard_id} cannot be replaced in place; starting rescue session")
    Metrics.count("Conduit/ShardRescues")

    spec = %{
      id: {:rescue, shard_id},
      start:
        {Ingress.ShardSession, :start_link,
         [[shard_id: shard_id, conduit_id: conduit_id, rescue?: true]]},
      restart: :temporary
    }

    case Horde.DynamicSupervisor.start_child(Ingress.ShardSupervisor, spec) do
      {:ok, pid} ->
        Map.put(rescues, shard_id, {pid, seen})

      other ->
        Logger.warning("rescue for shard #{shard_id} failed to start: #{inspect(other)}")
        rescues
    end
  end

  defp heal_rescue({_conduit_id, shard_id} = slot, {pid, spawned_seen}, seen, rescues) do
    case ShardHealth.heal_action(probe_shard(pid), seen - spawned_seen) do
      :skip ->
        {seen, rescues}

      :force_rebind ->
        GenServer.cast(pid, :force_rebind)
        {seen, rescues}

      :restart ->
        stop_rescue(shard_id, pid)
        {seen, spawn_rescue(slot, seen, Map.delete(rescues, shard_id))}
    end
  end

  defp reap_rescues(unhealthy, rescues) do
    Enum.reduce(rescues, %{}, fn {shard_id, entry}, acc ->
      if shard_id in unhealthy do
        Map.put(acc, shard_id, entry)
      else
        maybe_release_rescue(shard_id, entry, acc)
      end
    end)
  end

  defp maybe_release_rescue(shard_id, {pid, _spawned_seen} = entry, acc) do
    case named_session_serving(shard_id) do
      {:ok, named} ->
        GenServer.cast(named, :reassert_binding)
        stop_rescue(shard_id, pid)
        acc

      :not_serving ->
        Map.put(acc, shard_id, entry)
    end
  end

  defp named_session_serving(shard_id) do
    with [{pid, _}] <- Horde.Registry.lookup(Ingress.Registry, {:shard, shard_id}),
         %{bound: true} <- probe_shard(pid) do
      {:ok, pid}
    else
      _ -> :not_serving
    end
  end

  defp stop_rescue(shard_id, pid) do
    Logger.info("stopping rescue session for shard #{shard_id}")

    case Horde.DynamicSupervisor.terminate_child(Ingress.ShardSupervisor, pid) do
      :ok -> :ok
      {:error, _} -> stop_orphan_shard(shard_id, pid)
    end
  end
end
