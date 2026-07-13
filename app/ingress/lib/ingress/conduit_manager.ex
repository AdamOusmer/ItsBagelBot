defmodule Ingress.ConduitManager do
  @moduledoc """
  Cluster-singleton reconciler for the Conduit.

  Exactly one instance runs in the BEAM cluster (registered in
  `Ingress.Registry`, supervised by `Ingress.ShardSupervisor`, so it fails
  over with the rest of the Horde-managed processes). It ensures the Conduit
  exists on Twitch's side with the desired shard count (sourced from
  `Ingress.ShardScaler`), then converges the running `Ingress.ShardSession`
  processes to match — starting missing shards and stopping excess ones.

  Reconciliation repeats periodically. Each tick reads Twitch's per-shard
  snapshot once and uses it twice: to gate shard starts (never start into a
  slot Twitch says is being served — that is how rolling deploys used to
  spawn duplicate copies) and to heal any slot Twitch reports unhealthy
  (dead binding, wedged session, blocked replacement — see the rescue
  section below). Conduit writes still happen only when the desired shard
  count changes.
  """

  use GenServer
  require Logger

  alias Ingress.Metrics
  alias Ingress.ShardHealth
  alias Ingress.ShardScaler
  alias Ingress.ShardSession
  alias Ingress.Twitch.Api

  # Also the shard-health poll cadence: worst-case blackhole detection is one
  # interval, and escalation to a replacement/rescue is two.
  @reconcile_interval_ms 15_000
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
     %{
       conduit_id: nil,
       applied_shard_count: nil,
       adopted_scaler: nil,
       # shard_id => consecutive reconcile ticks observed unhealthy on Twitch
       unhealthy_counts: %{},
       # shard_id => {rescue pid, seen count when the rescue was spawned}
       rescues: %{}
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
        snapshot = shard_snapshot(state.conduit_id)
        applied = converge_shards(state.conduit_id, desired, state.applied_shard_count, snapshot)
        state = run_health_pass(%{state | applied_shard_count: applied}, desired, snapshot)
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
  # Twitch's per-shard snapshot backing both the duplicate-start gate and the
  # health pass; fetched once per tick.
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

  # A shard is only started when Twitch does not report its slot enabled: an
  # enabled slot has a live serving socket somewhere even if the registry
  # shows a hole (registry state lags membership changes during a rolling
  # deploy — starting into the hole is how duplicate copies used to spawn
  # and race the serving one). Without a snapshot, fall back to starting
  # every slot; the registry still deduplicates the common case.
  defp start_missing_shards(conduit_id, desired, snapshot) do
    for shard_id <- startable_ids(snapshot, desired), do: start_shard(conduit_id, shard_id)
  end

  defp startable_ids({:ok, shards}, desired), do: ShardHealth.startable_ids(shards, desired)
  defp startable_ids(:error, desired), do: Enum.to_list(0..(desired - 1))

  # :started only when a fresh session actually spawned. :already_started is
  # normal during converge (the shard runs), but right after a terminate it
  # means the registration is wedged on a pid nothing can remove — callers on
  # the restart path treat :blocked as the cue to escalate to a rescue.
  defp start_shard(conduit_id, shard_id) do
    spec = {Ingress.ShardSession, shard_id: shard_id, conduit_id: conduit_id}

    case Horde.DynamicSupervisor.start_child(Ingress.ShardSupervisor, spec) do
      {:ok, _pid} ->
        Logger.info("started shard #{shard_id}")
        :started

      {:error, {:already_started, _pid}} ->
        :blocked

      :ignore ->
        :blocked

      {:error, reason} ->
        Logger.warning("shard #{shard_id} start failed: #{inspect(reason)}")
        :blocked
    end
  end

  # Twitch silently drops events routed to a shard slot whose transport is not
  # enabled — a conduit never rebalances a subscription to a healthy shard —
  # and a slot can die without any local process noticing (its socket keeps
  # receiving keepalives after the binding moved or died with another copy).
  # So reconciliation asks Twitch for its per-shard view and repairs every
  # slot it reports unhealthy. Decisions live in `Ingress.ShardHealth`.
  defp run_health_pass(state, desired, {:ok, shards}) do
    unhealthy = ShardHealth.unhealthy_ids(shards, desired)
    {counts, rescues} = heal_all(state, unhealthy)
    rescues = reap_rescues(unhealthy, rescues)
    %{state | unhealthy_counts: counts, rescues: rescues}
  end

  # No snapshot, no verdicts: carry the counts unchanged rather than treating
  # a Helix hiccup as five healthy shards.
  defp run_health_pass(state, _desired, :error), do: state

  # Heals every Twitch-unhealthy shard, threading the consecutive-unhealthy
  # counts and the rescue table. Shards Twitch reports healthy drop out of the
  # counts map, so a heal that works resets the escalation clock.
  defp heal_all(state, unhealthy) do
    Enum.reduce(unhealthy, {%{}, state.rescues}, fn shard_id, {counts, rescues} ->
      seen = Map.get(state.unhealthy_counts, shard_id, 0) + 1
      {count, rescues} = heal_shard(state.conduit_id, shard_id, seen, rescues)
      {Map.put(counts, shard_id, count), rescues}
    end)
  end

  defp heal_shard(conduit_id, shard_id, seen, rescues) do
    case Horde.Registry.lookup(Ingress.Registry, {:shard, shard_id}) do
      [] -> heal_unregistered(conduit_id, shard_id, seen, rescues)
      [{pid, _}] -> heal_registered(conduit_id, shard_id, pid, seen, rescues)
    end
  end

  # Nothing registered: normally this tick's start_missing_shards pass covers
  # it. If the shard is still unhealthy after that had its chance, starting is
  # blocked (supervisor wedge, placement failure) — bring up a rescue.
  defp heal_unregistered(conduit_id, shard_id, seen, rescues) do
    if ShardHealth.escalate?(seen) do
      ensure_rescue(conduit_id, shard_id, seen, rescues)
    else
      {seen, rescues}
    end
  end

  defp heal_registered(conduit_id, shard_id, pid, seen, rescues) do
    case ShardHealth.heal_action(probe_shard(pid), seen) do
      :skip ->
        {seen, rescues}

      :force_rebind ->
        Logger.warning("shard #{shard_id} unhealthy on Twitch but bound locally; forcing re-bind")
        Metrics.count("Conduit/ShardRebinds")
        GenServer.cast(pid, :force_rebind)
        {seen, rescues}

      :restart ->
        restart_registered(conduit_id, shard_id, pid, seen, rescues)
    end
  end

  # A successful restart resets the observation count: the replacement session
  # gets the full backstop window before it can be judged wedged. A blocked
  # restart (the registration or supervision is wedged on a pid nothing can
  # remove) escalates to a rescue session, which needs no name at all.
  defp restart_registered(conduit_id, shard_id, pid, seen, rescues) do
    Logger.warning("shard #{shard_id} unhealthy on Twitch (#{seen} ticks); replacing session")
    Metrics.count("Conduit/ShardRestarts")

    case restart_shard(conduit_id, shard_id, pid) do
      :started -> {0, rescues}
      :blocked -> ensure_rescue(conduit_id, shard_id, seen, rescues)
    end
  end

  defp restart_shard(conduit_id, shard_id, pid) do
    case Horde.DynamicSupervisor.terminate_child(Ingress.ShardSupervisor, pid) do
      :ok -> :ok
      {:error, :not_found} -> stop_orphan_shard(shard_id, pid)
      {:error, reason} -> Logger.warning("stop shard #{shard_id} failed: #{inspect(reason)}")
    end

    start_shard(conduit_id, shard_id)
  end

  defp probe_shard(pid) do
    ShardSession.status(pid)
  catch
    :exit, _ -> :unreachable
  end

  # --- rescue sessions --------------------------------------------------------
  #
  # Last line of defense: when a shard slot stays dead on Twitch and the named
  # session cannot even be replaced (Horde registration or supervision wedged
  # on a dead pid), an unnamed session is started instead. It binds the shard
  # on Twitch — the binding PATCH, not the registry, decides who receives
  # events — so the slot serves again no matter what the cluster metadata
  # says. Once Twitch reports the slot healthy and a live named session holds
  # a binding, the named session re-asserts and the rescue is stopped.

  defp ensure_rescue(conduit_id, shard_id, seen, rescues) do
    case Map.get(rescues, shard_id) do
      nil -> {seen, spawn_rescue(conduit_id, shard_id, seen, rescues)}
      {pid, spawned_seen} -> heal_rescue(conduit_id, shard_id, pid, seen, spawned_seen, rescues)
    end
  end

  defp spawn_rescue(conduit_id, shard_id, seen, rescues) do
    Logger.warning("shard #{shard_id} cannot be replaced in place; starting rescue session")
    Metrics.count("Conduit/ShardRescues")

    spec = %{
      id: {:rescue, shard_id},
      start:
        {Ingress.ShardSession, :start_link,
         [[shard_id: shard_id, conduit_id: conduit_id, rescue?: true]]},
      # :temporary — a crashed rescue is not restarted blindly; the next
      # health pass decides whether one is still needed.
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

  # A rescue is judged by the same rules as a named session, on its own clock
  # (ticks since it was spawned): give it the settle window, force a re-bind
  # if it claims a binding Twitch says is dead, replace it if it stays stuck.
  defp heal_rescue(conduit_id, shard_id, pid, seen, spawned_seen, rescues) do
    case ShardHealth.heal_action(probe_shard(pid), seen - spawned_seen) do
      :skip ->
        {seen, rescues}

      :force_rebind ->
        GenServer.cast(pid, :force_rebind)
        {seen, rescues}

      :restart ->
        stop_rescue(shard_id, pid)
        {seen, spawn_rescue(conduit_id, shard_id, seen, Map.delete(rescues, shard_id))}
    end
  end

  # Twitch reports a rescued slot healthy again: hand it back to the named
  # session when one is alive and bound (re-assert makes Twitch's routing
  # follow it before the rescue socket closes). While no named session
  # serves, the rescue IS the shard; keep it.
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
