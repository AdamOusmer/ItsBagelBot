# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.AdminRpc do
  use Ingress.RpcServer, log: "admin rpc"

  alias Ingress.{Capacity, JSON, ShardInventory, ShardScaler, Singleton, TrialReceiver}

  @call_timeout_ms 2_000

  @impl true
  def request(%{body: _body}) do
    {:reply, JSON.encode(snapshot())}
  end

  def snapshot do
    scaler = ShardScaler.status()
    desired = scaler.desired
    nodes = [node() | Node.list()] |> Enum.uniq()
    trials = TrialReceiver.cluster_status(nodes)

    inventory = ShardInventory.by_shard()
    registered_ids = registered_shard_ids()

    all_ids =
      (Map.keys(inventory) ++ registered_ids ++ Enum.to_list(0..(max(desired, 1) - 1)))
      |> Enum.uniq()
      |> Enum.sort()

    shards = Enum.map(all_ids, &shard_status(&1, inventory, registered_ids))

    %{
      generated_at: DateTime.utc_now(),
      reporter: node(),
      nodes: nodes,
      capacity: Capacity.snapshot(length(nodes)),
      # Legacy key: older console code still reads shard_count.
      shard_count: desired,
      desired_count: desired,
      target: scaler.target,
      min_shards: scaler.min_shards,
      max_shards: scaler.max_shards,
      autoscale: scaler.autoscale,
      max_load: scaler.max_load,
      max_load_shard_id: scaler.max_load_shard_id,
      conduit_manager: manager_status(),
      shards: shards,
      trial_loads: trials.loads,
      trial_burst_loads: trials.bursts,
      trial_sockets: trials.sockets
    }
  end

  defp registered_shard_ids do
    Horde.Registry.select(Ingress.Registry, [
      {{{:shard, :"$1"}, :_, :_}, [], [:"$1"]}
    ])
  end

  defp shard_status(shard_id, inventory, registered_ids) do
    case Map.fetch(inventory, shard_id) do
      {:ok, status} -> status
      :error -> vacant_status(shard_id, shard_id in registered_ids)
    end
  end

  defp vacant_status(shard_id, true),
    do: %{shard_id: shard_id, state: "unresponsive", managed: true}

  defp vacant_status(shard_id, false),
    do: %{shard_id: shard_id, state: "unregistered", managed: false}

  defp manager_status do
    case Singleton.call(:conduit_manager, :status, @call_timeout_ms, &manager_silence/1) do
      %{state: _reported} = silence -> silence
      status -> Map.put(status, :state, "running")
    end
  end

  defp manager_silence(:down), do: %{state: "down"}
  defp manager_silence({:unresponsive, pid}), do: %{state: "unresponsive", node: node(pid)}
end
