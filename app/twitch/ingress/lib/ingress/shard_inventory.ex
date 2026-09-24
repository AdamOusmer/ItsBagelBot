# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ShardInventory do
  alias Ingress.ShardSession

  @probe_timeout_ms 2_000

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

  def unmanaged do
    for {pid, status} <- sessions(), not registered?(pid, status), do: {pid, status}
  end

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
