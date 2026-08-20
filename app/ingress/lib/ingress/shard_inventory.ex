# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ShardInventory do
  @moduledoc """
  What the supervisor is actually running, as opposed to what the registry
  says it is running.

  Every other discovery path in the ingress enumerates shards through
  `Ingress.Registry`, and that is precisely the view that can be wrong: a
  registry CRDT merge after a netsplit heal drops registrations for processes
  that are still alive and serving. On 2026-08-20 that left shard 0 serving
  under no name (reported to the console as `unregistered` while events flowed
  through it) and shard 4 holding a bound socket for a conduit slot a
  scale-down had already deleted -- invisible to `stop_excess_shards/1`, which
  can only reap what the registry can show it.

  `Horde.DynamicSupervisor`'s child list survives that merge, so it is the view
  used to find sessions the registry has lost. Probes run in parallel with a
  bounded timeout: a wedged session must not stall the sweep or the admin
  snapshot, and one that cannot answer is simply left out rather than guessed
  at -- the rescue path in `Ingress.ConduitManager` owns wedged slots.
  """

  alias Ingress.ShardSession

  @probe_timeout_ms 2_000

  @doc """
  Every live `Ingress.ShardSession` in the cluster as `{pid, status}`. Sessions
  that do not answer a status call within the probe timeout are omitted.
  """
  def sessions do
    Ingress.ShardSupervisor
    |> Horde.DynamicSupervisor.which_children()
    |> Enum.filter(&session_child?/1)
    |> Task.async_stream(&probe_child/1,
      timeout: @probe_timeout_ms + 500,
      on_timeout: :kill_task
    )
    |> Enum.flat_map(fn
      {:ok, {_pid, %{shard_id: _}} = entry} -> [entry]
      _dead_or_unreachable -> []
    end)
  catch
    :exit, _ -> []
  end

  @doc """
  Sessions holding no registry entry under their own shard id, as
  `{pid, status}`. A session whose name is held by a *different* pid counts as
  unmanaged too: it is running outside anything the cluster can steer.
  """
  def unmanaged do
    for {pid, status} <- sessions(), not registered?(pid, status), do: {pid, status}
  end

  @doc """
  Live sessions keyed by shard id, each tagged `managed: true` when the
  registry maps its shard id to that pid.

  Where two sessions claim one id -- a rescue alongside a named session, or a
  duplicate mid-resolution -- the managed one wins. `false` sorts before `true`
  in Erlang term order, so sorting on the flag puts the managed copy last and
  `Map.new/2` keeps it.
  """
  def by_shard do
    sessions()
    |> Enum.map(fn {pid, status} -> Map.put(status, :managed, registered?(pid, status)) end)
    |> Enum.sort_by(& &1.managed)
    |> Map.new(&{&1.shard_id, &1})
  end

  defp session_child?({_id, pid, :worker, [ShardSession]}) when is_pid(pid), do: true
  defp session_child?(_child), do: false

  defp probe_child({_id, pid, _type, _modules}), do: {pid, probe(pid)}

  defp probe(pid) do
    ShardSession.status(pid, @probe_timeout_ms)
  catch
    :exit, _ -> :unreachable
  end

  defp registered?(pid, %{shard_id: shard_id}) do
    match?([{^pid, _}], Horde.Registry.lookup(Ingress.Registry, {:shard, shard_id}))
  end
end
