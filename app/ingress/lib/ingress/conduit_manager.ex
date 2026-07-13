defmodule Ingress.ConduitManager do
  @moduledoc """
  Cluster-singleton reconciler for the Conduit.

  Exactly one instance runs in the BEAM cluster (registered in
  `Ingress.Registry`, supervised by `Ingress.ShardSupervisor`, so it fails
  over with the rest of the Horde-managed processes). It ensures the Conduit
  exists on Twitch's side with the desired shard count (sourced from
  `Ingress.ShardScaler`), then converges the running `Ingress.ShardSession`
  processes to match — starting missing shards and stopping excess ones.

  Reconciliation repeats periodically to heal local shards whose supervisor
  gave up. Twitch is updated only when the internally-owned desired shard count
  changes; stable reconciliation ticks never spend Helix quota.
  """

  use GenServer
  require Logger

  alias Ingress.Metrics
  alias Ingress.ShardHealth
  alias Ingress.ShardScaler
  alias Ingress.ShardSession
  alias Ingress.Twitch.Api

  @reconcile_interval_ms 30_000
  @retry_interval_ms 5_000
  # How long a direct stop of an unsupervised (orphan) shard may take.
  @orphan_stop_timeout_ms 5_000

  def start_link(_opts) do
    GenServer.start_link(__MODULE__, [], name: via())
  end

  def via, do: {:via, Horde.Registry, {Ingress.Registry, :conduit_manager}}

  @impl true
  def init(_) do
    {:ok,
     %{conduit_id: nil, applied_shard_count: nil, adopted_scaler: nil, unhealthy_counts: %{}},
     {:continue, :reconcile}}
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

  # Convergence acts only on a live answer from the scaler singleton:
  # `ShardScaler.desired/0`'s fallback is the config floor, and resizing the
  # conduit down to that while the scaler is between homes (failover, deploy)
  # would drop autoscaled shard bindings. Holding for a retry interval is
  # always safe — the running shards keep serving.
  #
  # A scaler answering under a pid we have not adopted yet is fresh (first boot
  # or a restart that forgot the autoscaled target), so Twitch's recorded count
  # is adopted before its answer is allowed to converge anything.
  defp converge_with_scaler(state) do
    case ShardScaler.fetch_desired() do
      {:ok, _desired, scaler} when scaler != state.adopted_scaler ->
        adopt_then_converge(state, scaler)

      {:ok, desired, _scaler} ->
        applied = converge_shards(state.conduit_id, desired, state.applied_shard_count)
        counts = heal_unhealthy_shards(state.conduit_id, desired, state.unhealthy_counts)
        Process.send_after(self(), :reconcile, @reconcile_interval_ms)
        %{state | applied_shard_count: applied, unhealthy_counts: counts}

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

  # Twitch's recorded shard count is the only survivor of a singleton failover
  # or deploy: the scaler restarts at the config floor, so without adoption the
  # next converge would shrink an autoscaled conduit and drop shard bindings
  # until the autoscaler climbed back. Only raises the target (set_target
  # clamps to max_shards); a count at or below the current desired changes
  # nothing.
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

  # Converge the Conduit (Twitch side) and local ShardSession processes to
  # exactly `desired` shards. Grows or shrinks as needed.
  #
  # Order of operations:
  #   1. If `desired` changed, resize the Conduit on Twitch's side so it
  #      accepts exactly that many shard bindings.
  #   2. Stop sessions for shard IDs >= desired (the Conduit no longer has
  #      slots for them; stopping is safe because :transient restart means a
  #      deliberate shutdown will not restart the session).
  #   3. Start any missing sessions for shard IDs 0..(desired-1).
  defp converge_shards(conduit_id, desired, applied) when desired > 0 do
    applied = maybe_resize_conduit(conduit_id, desired, applied)

    stop_excess_shards(desired)
    start_missing_shards(conduit_id, desired)
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

  # Stop ShardSession processes for shard IDs >= desired. We look them up in
  # the cluster-wide registry; if they live on a remote node Horde still
  # terminates them via the DynamicSupervisor.
  defp stop_excess_shards(desired) do
    # Discover running shard IDs by scanning the registry for {:shard, _} keys.
    running_ids =
      Horde.Registry.select(Ingress.Registry, [
        {{{:shard, :"$1"}, :_, :_}, [], [:"$1"]}
      ])

    for shard_id <- running_ids, shard_id >= desired do
      case Horde.Registry.lookup(Ingress.Registry, {:shard, shard_id}) do
        [{pid, _}] ->
          Logger.info("stopping excess shard #{shard_id} (desired=#{desired})")

          case Horde.DynamicSupervisor.terminate_child(Ingress.ShardSupervisor, pid) do
            :ok ->
              :ok

            {:error, :not_found} ->
              stop_orphan_shard(shard_id, pid)

            {:error, reason} ->
              Logger.warning("stop shard #{shard_id} failed: #{inspect(reason)}")
          end

        [] ->
          :ok
      end
    end
  end

  # `terminate_child` answers :not_found for a shard the supervisor does not
  # track: duplicate-shard takeover re-registers the surviving process directly
  # in the registry, so the supervisor CRDT never learns its pid and the shard
  # would otherwise outlive every scale-down, bind-looping against a conduit
  # that has no slot for it. Stop the process itself. GenServer.stop rather
  # than an exit signal: the session traps exits and would absorb a :shutdown
  # signal as an info message.
  defp stop_orphan_shard(shard_id, pid) do
    Logger.warning("stopping orphan shard #{shard_id} (registered but unsupervised)")

    try do
      GenServer.stop(pid, :normal, @orphan_stop_timeout_ms)
    catch
      :exit, reason ->
        Logger.warning("orphan shard #{shard_id} stop failed: #{inspect(reason)}")
    end
  end

  defp start_missing_shards(conduit_id, desired) do
    for shard_id <- 0..(desired - 1), do: start_shard(conduit_id, shard_id)
  end

  defp start_shard(conduit_id, shard_id) do
    spec = {Ingress.ShardSession, shard_id: shard_id, conduit_id: conduit_id}

    case Horde.DynamicSupervisor.start_child(Ingress.ShardSupervisor, spec) do
      {:ok, _pid} -> Logger.info("started shard #{shard_id}")
      {:error, {:already_started, _pid}} -> :ok
      :ignore -> :ok
      {:error, reason} -> Logger.warning("shard #{shard_id} start failed: #{inspect(reason)}")
    end
  end

  # Twitch silently drops events routed to a shard slot whose transport is not
  # enabled — a conduit never rebalances a subscription to a healthy shard —
  # and a slot can die without any local process noticing (its socket keeps
  # receiving keepalives after the binding moved or died with another copy).
  # So reconciliation asks Twitch for its per-shard view and repairs every
  # slot it reports unhealthy. Decisions live in `Ingress.ShardHealth`.
  #
  # Returns the consecutive-unhealthy-tick count per shard id, carried in the
  # manager state so persistent unhealth escalates to a restart. Shards Twitch
  # reports healthy drop out of the map.
  defp heal_unhealthy_shards(conduit_id, desired, counts) do
    case Api.get_shards(conduit_id) do
      {:ok, shards} ->
        shards
        |> ShardHealth.unhealthy_ids(desired)
        |> Map.new(fn shard_id ->
          seen = Map.get(counts, shard_id, 0) + 1
          {shard_id, heal_shard(conduit_id, shard_id, seen)}
        end)

      {:error, reason} ->
        Logger.warning("shard health poll failed: #{inspect(reason)}")
        counts
    end
  end

  defp heal_shard(conduit_id, shard_id, seen) do
    case Horde.Registry.lookup(Ingress.Registry, {:shard, shard_id}) do
      # Nothing registered: this tick's start_missing_shards pass (or the next
      # one) starts the shard; healing has nothing to act on yet.
      [] ->
        seen

      [{pid, _}] ->
        action = ShardHealth.heal_action(probe_shard(pid), seen)
        apply_heal(action, conduit_id, shard_id, pid, seen)
    end
  end

  # A restart resets the observation count: the replacement session gets the
  # full backstop window before it can be judged wedged.
  defp apply_heal(:restart, conduit_id, shard_id, pid, seen) do
    Logger.warning(
      "shard #{shard_id} unhealthy on Twitch (#{seen} ticks); replacing local session"
    )

    Metrics.count("Conduit/ShardRestarts")
    restart_shard(conduit_id, shard_id, pid)
    0
  end

  defp apply_heal(:force_rebind, _conduit_id, shard_id, pid, seen) do
    Logger.warning("shard #{shard_id} unhealthy on Twitch but bound locally; forcing re-bind")
    Metrics.count("Conduit/ShardRebinds")
    GenServer.cast(pid, :force_rebind)
    seen
  end

  defp apply_heal(:skip, _conduit_id, _shard_id, _pid, seen), do: seen

  defp probe_shard(pid) do
    ShardSession.status(pid)
  catch
    :exit, _ -> :unreachable
  end

  defp restart_shard(conduit_id, shard_id, pid) do
    case Horde.DynamicSupervisor.terminate_child(Ingress.ShardSupervisor, pid) do
      :ok -> :ok
      {:error, :not_found} -> stop_orphan_shard(shard_id, pid)
      {:error, reason} -> Logger.warning("stop shard #{shard_id} failed: #{inspect(reason)}")
    end

    start_shard(conduit_id, shard_id)
  end
end
