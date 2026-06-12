defmodule Ingress.AdminRpc do
  @moduledoc """
  NATS request-reply endpoint for the admin tool (subject from
  `NATS_ADMIN_SUBJECT`). Any ingress node can answer: shards are reached
  through the Horde registry, so the snapshot covers the whole BEAM cluster
  regardless of which replica picked up the request.

  The reply is a JSON document with the cluster membership, the
  conduit-manager singleton, and one entry per configured shard. Shards that
  are registered but mid-connect can be slow to answer; they are reported as
  `unresponsive` rather than holding up the reply.
  """

  use Gnat.Server
  require Logger

  alias Ingress.Config

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
    shard_count = Config.conduit_shard_count()

    shards =
      0..(shard_count - 1)
      |> Task.async_stream(&shard_status/1,
        timeout: @call_timeout_ms + 500,
        on_timeout: :kill_task
      )
      |> Enum.zip(0..(shard_count - 1))
      |> Enum.map(fn
        {{:ok, status}, _shard_id} -> status
        {{:exit, _reason}, shard_id} -> %{shard_id: shard_id, state: "unresponsive"}
      end)

    %{
      generated_at: DateTime.utc_now(),
      reporter: node(),
      nodes: [node() | Node.list()],
      shard_count: shard_count,
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
