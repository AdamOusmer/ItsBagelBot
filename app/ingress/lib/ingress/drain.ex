defmodule Ingress.Drain do
  @moduledoc """
  Make-before-break shard handoff for planned shutdown.

  `Ingress.Application.prep_stop/1` calls `run/0` on SIGTERM, before the
  supervision tree stops. For every shard session running on this node:

    1. Release its cluster registration (`:release_name`) — the socket keeps
       serving, but the name is free for a successor.
    2. Start a successor session on a surviving node. The `{:draining, node}`
       marker registered here keeps `Ingress.ShardDistribution` from placing
       it back on this node.
    3. Wait for the successor to bind. The Conduit routes each event to
       whichever session bound last, so the slot switches to the successor
       the moment its PATCH lands — no gap, no drop.
    4. Stop the local session; its socket is now an idle superseded one.

  Handoffs run concurrently and are deadline-bounded so the whole drain fits
  inside the pod's termination grace period. Every failure path degrades to
  "keep serving until the tree stops", and the ConduitManager health pass is
  the floor for anything a drain could not hand off — including rescue
  sessions (unnamed, never handed off) and unplanned deaths (crash, OOM,
  node loss), which never run this path at all.
  """

  require Logger

  alias Ingress.{Config, Metrics, ShardSession}

  # Budget per shard for the successor to connect, welcome and bind. The
  # whole drain must fit inside terminationGracePeriodSeconds minus the
  # preStop sleep; handoffs run concurrently so this is also ~the total.
  @handoff_deadline_ms 12_000
  @poll_interval_ms 300
  @call_timeout_ms 2_000

  def run do
    marker = mark_draining()

    local_shards()
    |> Task.async_stream(&hand_off/1,
      timeout: @handoff_deadline_ms + 5_000,
      on_timeout: :kill_task,
      ordered: false
    )
    |> Stream.run()

    release_marker(marker)
    :ok
  rescue
    error ->
      Logger.error("drain failed: #{Exception.message(error)}; shutting down without handoff")
      :ok
  end

  # The marker must outlive each registering call, so it lives in a helper
  # process that survives until the drain finishes; its registration (and the
  # draining flag with it) dies with the pod at the latest.
  defp mark_draining do
    caller = self()

    pid =
      spawn(fn ->
        Horde.Registry.register(Ingress.Registry, {:draining, node()}, nil)
        send(caller, {:marked, self()})

        receive do
          :release -> :ok
        end
      end)

    receive do
      {:marked, ^pid} -> pid
    after
      @call_timeout_ms -> pid
    end
  end

  defp release_marker(pid), do: send(pid, :release)

  defp local_shards do
    Horde.Registry.select(Ingress.Registry, [
      {{{:shard, :"$1"}, :"$2", :_}, [], [{{:"$1", :"$2"}}]}
    ])
    |> Enum.filter(fn {_shard_id, pid} -> node(pid) == node() end)
  end

  defp hand_off({shard_id, pid}) do
    Logger.info("draining shard #{shard_id}")
    Metrics.count("Drain/Handoffs")
    release_name(pid)

    case start_successor(shard_id) do
      {:ok, successor} ->
        await_bound(successor, deadline())
        stop_session(pid)

      :error ->
        # No successor possible (no peers, start failed): keep serving until
        # the tree stops — every second counts — and let the health pass
        # re-establish the slot after this pod is gone.
        Logger.warning("shard #{shard_id}: no successor; serving until shutdown")
        Metrics.count("Drain/HandoffFailures")
    end
  end

  defp release_name(pid) do
    GenServer.call(pid, :release_name, @call_timeout_ms)
  catch
    :exit, _ -> :ok
  end

  # Placement is Horde's (drain-aware via the marker), but the start call
  # must run on a surviving node: a start_child issued here could not place
  # the child anywhere once this node's supervisor begins stopping. If the
  # marker has not replicated yet and placement lands back on this dying
  # node, tear that copy down and try again — one round of the registry's
  # sync interval is enough for the marker to arrive.
  defp start_successor(shard_id), do: start_successor(shard_id, 2)

  defp start_successor(shard_id, 0) do
    Logger.warning("successor for shard #{shard_id} kept landing on the draining node")
    :error
  end

  defp start_successor(shard_id, attempts) do
    spec = {ShardSession, shard_id: shard_id, conduit_id: Config.twitch_conduit_id()}

    case remote_start(spec) do
      {:ok, pid} when node(pid) != node() ->
        {:ok, pid}

      {:ok, pid} ->
        Horde.DynamicSupervisor.terminate_child(Ingress.ShardSupervisor, pid)
        Process.sleep(500)
        start_successor(shard_id, attempts - 1)

      other ->
        Logger.warning("successor for shard #{shard_id} did not start: #{inspect(other)}")
        :error
    end
  end

  defp remote_start(spec) do
    case Node.list() do
      [] ->
        {:error, :no_peers}

      [target | _] ->
        case :rpc.call(target, Horde.DynamicSupervisor, :start_child, [
               Ingress.ShardSupervisor,
               spec
             ]) do
          {:ok, pid} when is_pid(pid) -> {:ok, pid}
          {:error, {:already_started, pid}} -> {:ok, pid}
          other -> other
        end
    end
  end

  defp deadline, do: System.monotonic_time(:millisecond) + @handoff_deadline_ms

  defp await_bound(pid, deadline) do
    cond do
      bound?(pid) ->
        :ok

      System.monotonic_time(:millisecond) >= deadline ->
        # Close anyway: the successor keeps trying on its own, and the
        # health pass repairs the slot if it never manages to bind.
        :timeout

      true ->
        Process.sleep(@poll_interval_ms)
        await_bound(pid, deadline)
    end
  end

  defp bound?(pid) do
    match?(%{bound: true}, ShardSession.status(pid))
  catch
    :exit, _ -> false
  end

  defp stop_session(pid) do
    GenServer.stop(pid, :normal, @call_timeout_ms)
  catch
    :exit, _ -> :ok
  end
end
