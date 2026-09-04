# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.AdminRpc do
  @moduledoc """
  NATS request-reply endpoint for the admin tool (subject from
  `NATS_ADMIN_SUBJECT`). Any ingress node can answer: shards are reached
  through the Horde registry, so the snapshot covers the whole BEAM cluster
  regardless of which replica picked up the request.

  The reply is a JSON document with the cluster membership, the
  conduit-manager singleton, scaler state, and one entry per live shard.
  Shards that are registered but mid-connect can be slow to answer; they are
  reported as `unresponsive` rather than holding up the reply.

  Shards are enumerated from `Ingress.ShardInventory` (the supervisor's own
  child list) rather than from the registry alone, and each entry carries
  `managed`. A registry-only enumeration cannot see a session whose
  registration a CRDT merge dropped, which is how a shard serving live events
  came to be reported as an empty slot and a zombie shard above `desired` came
  to be reported as nothing at all.

  The `shard_count` field reflects the effective desired count from
  `Ingress.ShardScaler`, not the static config value. Additional top-level
  fields (`desired_count`, `target`, `min_shards`, `autoscale`) expose the
  scaler's view so the admin console can show the full picture without a
  separate RPC call. `max_load`/`max_load_shard_id` identify the single
  hottest shard from the scaler's last autoscale sample, for spotting a
  broadcaster concentrated on one shard even when aggregate load looks fine.
  """

  use Gnat.Server
  require Logger

  alias Ingress.{Capacity, JSON, ShardInventory, ShardScaler}

  @call_timeout_ms 2_000

  @impl true
  def request(%{body: _body}) do
    {:reply, JSON.encode(snapshot())}
  end

  @impl true
  def error(_message, error) do
    Logger.error("admin rpc error: #{inspect(error)}")
    :ok
  end

  def snapshot do
    scaler = ShardScaler.status()
    desired = scaler.desired
    nodes = [node() | Node.list()] |> Enum.uniq()

    # Three sources, because no single one is complete. The supervisor's child
    # list is the only view of sessions the registry has lost -- omit it and a
    # shard serving without a registration is reported as an empty slot while
    # events flow through it, and one above `desired` is not reported at all.
    # The registry still contributes ids whose session did not answer its probe
    # (reported `unresponsive`, not silently dropped), and the desired range
    # contributes slots that genuinely have nothing running. Enumerating past
    # `desired` is deliberate: during a shrink a shard may still be stopping.
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
      # shard_count mirrors desired_count for backwards compatibility with
      # any console code that reads the old field name.
      shard_count: desired,
      desired_count: desired,
      target: scaler.target,
      min_shards: scaler.min_shards,
      max_shards: scaler.max_shards,
      autoscale: scaler.autoscale,
      max_load: scaler.max_load,
      max_load_shard_id: scaler.max_load_shard_id,
      conduit_manager: manager_status(),
      shards: shards
    }
  end

  defp registered_shard_ids do
    Horde.Registry.select(Ingress.Registry, [
      {{{:shard, :"$1"}, :_, :_}, [], [:"$1"]}
    ])
  end

  # A live session answers for its own slot, carrying `managed` so the console
  # can distinguish a shard the cluster is steering from one merely running.
  # With no session, the registry decides which of the two silences this is: a
  # name pointing at a process that would not answer its probe is
  # `unresponsive`, an empty slot is `unregistered`.
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
    case Horde.Registry.lookup(Ingress.Registry, :conduit_manager) do
      [{pid, _}] ->
        try do
          pid
          |> GenServer.call(:status, @call_timeout_ms)
          |> Map.put(:state, "running")
        catch
          :exit, _ -> %{state: "unresponsive", node: node(pid)}
        end

      [] ->
        %{state: "down"}
    end
  end
end
