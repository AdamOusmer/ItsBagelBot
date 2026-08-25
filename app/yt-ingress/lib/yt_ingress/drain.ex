# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.Drain do
  @moduledoc """
  Make-before-break chat session handoff for planned shutdown.

  `YtIngress.Application.prep_stop/1` calls `run/0` on SIGTERM, before the
  supervision tree stops. For every chat session running on this node:

    1. Release its cluster registration (`:release_name`) — the stream keeps
       serving, but the name is free for a successor.
    2. Start a successor session on a surviving node for the same live chat.
       The `{:draining, node}` marker registered here keeps
       `YtIngress.ChatDistribution` from placing it back on this node. The
       successor cold-connects, which replays YouTube's bounded history
       window once — safe because consumers de-duplicate on `msg_id`.
    3. Wait until the successor reports connected (first page token seen).
    4. Stop the local session; its HTTP/2 channel is now redundant.

   Unlike a Twitch shard handoff there is no server-side binding to race:
   both sessions streaming briefly is safe — consumers de-duplicate on
   `msg_id` — so "successor connected" is a courtesy wait, not a correctness
   requirement. Every failure path degrades to "keep serving until the tree
   stops", and the watcher's next reconcile tick re-establishes anything a
   drain could not hand off.

  Handoffs run concurrently and are deadline-bounded so the whole drain fits
  inside the pod's termination grace period.
  """

  require Logger

  alias YtIngress.{ChatSession, Metrics}

  # Budget per session for the successor to connect. The whole drain must fit
  # inside terminationGracePeriodSeconds minus the preStop sleep; handoffs run
  # concurrently so this is also ~the total.
  @handoff_deadline_ms 12_000
  @poll_interval_ms 300
  @call_timeout_ms 2_000

  def run do
    marker = mark_draining()

    local_sessions()
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
        Horde.Registry.register(YtIngress.Registry, {:draining, node()}, nil)
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

  defp local_sessions do
    Horde.Registry.select(YtIngress.Registry, [
      {{{:chat, :"$1"}, :"$2", :_}, [], [{{:"$1", :"$2"}}]}
    ])
    |> Enum.filter(fn {_channel_id, pid} -> node(pid) == node() end)
  end

  defp hand_off({channel_id, pid}) do
    Logger.info("draining chat session #{channel_id}")
    Metrics.count("Drain/Handoffs")

    snapshot = session_snapshot(pid)
    release_name(pid)

    case start_successor(channel_id, snapshot) do
      {:ok, successor} ->
        await_connected(successor, deadline())
        stop_session(pid)

      :error ->
        # No successor possible (no peers, start failed): keep serving until
        # the tree stops and let the watcher re-establish after this pod is
        # gone.
        Logger.warning("session #{channel_id}: no successor; serving until shutdown")
        Metrics.count("Drain/HandoffFailures")
    end
  end

  defp session_snapshot(pid) do
    ChatSession.status(pid, @call_timeout_ms)
  catch
    :exit, _ -> nil
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
  defp start_successor(channel_id, snapshot), do: start_successor(channel_id, snapshot, 2)

  defp start_successor(_channel_id, _snapshot, 0) do
    Logger.warning("successor kept landing on the draining node")
    :error
  end

  defp start_successor(channel_id, snapshot, attempts) do
    live_chat_id = snapshot && Map.get(snapshot, :live_chat_id)

    if live_chat_id do
      spec = {
        ChatSession,
        registry_key: {:chat, channel_id},
        channel_id: channel_id,
        live_chat_id: live_chat_id
      }

      case remote_start(spec) do
        {:ok, pid} when node(pid) != node() ->
          {:ok, pid}

        {:ok, pid} ->
          Horde.DynamicSupervisor.terminate_child(YtIngress.ChatSupervisor, pid)
          Process.sleep(500)
          start_successor(channel_id, snapshot, attempts - 1)

        other ->
          Logger.warning("successor for #{channel_id} did not start: #{inspect(other)}")
          :error
      end
    else
      # No usable snapshot: the watcher's next tick rebuilds the session from
      # discovery instead of us guessing at ids here.
      Logger.warning("session #{channel_id}: no snapshot; leaving re-establishment to the watcher")
      :error
    end
  end

  defp remote_start(spec) do
    case Node.list() do
      [] ->
        {:error, :no_peers}

      [target | _] ->
        case :rpc.call(target, Horde.DynamicSupervisor, :start_child, [
               YtIngress.ChatSupervisor,
               spec
             ]) do
          {:ok, pid} when is_pid(pid) -> {:ok, pid}
          {:error, {:already_started, pid}} -> {:ok, pid}
          other -> other
        end
    end
  end

  defp deadline, do: System.monotonic_time(:millisecond) + @handoff_deadline_ms

  defp await_connected(pid, deadline) do
    cond do
      connected?(pid) ->
        :ok

      System.monotonic_time(:millisecond) >= deadline ->
        # Close anyway: the successor keeps trying on its own, and the
        # watcher repairs ownership if it never connects.
        :timeout

      true ->
        Process.sleep(@poll_interval_ms)
        await_connected(pid, deadline)
    end
  end

  defp connected?(pid) do
    match?(%{connected?: true}, ChatSession.status(pid, @call_timeout_ms))
  catch
    :exit, _ -> false
  end

  defp stop_session(pid) do
    GenServer.stop(pid, :normal, @call_timeout_ms)
  catch
    :exit, _ -> :ok
  end
end
