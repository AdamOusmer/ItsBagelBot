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

  alias Ingress.{Capacity, ShardScaler}

  @call_timeout_ms 2_000

  @impl true
  def request(%{body: _body}) do
    {:reply, Jason.encode!(snapshot())}
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

    # Enumerate all currently-registered shards (not just 0..desired-1); during
    # a shrink a shard may still be stopping, and the snapshot should include it
    # so the console shows an accurate in-flight view.
    registered_ids =
      Horde.Registry.select(Ingress.Registry, [
        {{{:shard, :"$1"}, :_, :_}, [], [:"$1"]}
      ])

    all_ids =
      (registered_ids ++ Enum.to_list(0..(max(desired, 1) - 1)))
      |> Enum.uniq()
      |> Enum.sort()

    shards =
      all_ids
      |> Task.async_stream(&shard_status/1,
        timeout: @call_timeout_ms + 500,
        on_timeout: :kill_task
      )
      |> Enum.zip(all_ids)
      |> Enum.map(fn
        {{:ok, status}, _shard_id} -> status
        {{:exit, _reason}, shard_id} -> %{shard_id: shard_id, state: "unresponsive"}
      end)

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

  defp shard_status(shard_id) do
    case Horde.Registry.lookup(Ingress.Registry, {:shard, shard_id}) do
      [{pid, _}] ->
        try do
          Ingress.ShardSession.status(pid, @call_timeout_ms)
        catch
          :exit, _ -> %{shard_id: shard_id, state: "unresponsive", node: node(pid)}
        end

      [] ->
        %{shard_id: shard_id, state: "unregistered"}
    end
  end

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
