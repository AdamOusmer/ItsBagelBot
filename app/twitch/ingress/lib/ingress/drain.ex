# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Drain do
  require Logger

  alias Ingress.Config.Twitch, as: TwitchConfig
  alias Ingress.{Metrics, ShardSession}

  # Must fit the pod's terminationGracePeriodSeconds minus the preStop sleep.
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
        Logger.warning("shard #{shard_id}: no successor; serving until shutdown")
        Metrics.count("Drain/HandoffFailures")
    end
  end

  defp release_name(pid) do
    GenServer.call(pid, :release_name, @call_timeout_ms)
  catch
    :exit, _ -> :ok
  end

  defp start_successor(shard_id), do: start_successor(shard_id, 2)

  defp start_successor(shard_id, 0) do
    Logger.warning("successor for shard #{shard_id} kept landing on the draining node")
    :error
  end

  defp start_successor(shard_id, attempts) do
    spec = {ShardSession, shard_id: shard_id, conduit_id: TwitchConfig.conduit_id()}

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
