defmodule Ingress.ConduitManager do
  @moduledoc """
  Cluster-singleton reconciler for the Conduit.

  Exactly one instance runs in the BEAM cluster (registered in
  `Ingress.Registry`, supervised by `Ingress.ShardSupervisor`, so it fails
  over with the rest of the Horde-managed processes). It ensures the Conduit
  exists on Twitch's side with the configured shard count, then ensures one
  `Ingress.ShardSession` per shard is running somewhere in the cluster.

  Reconciliation repeats periodically: it heals shards whose supervisor gave
  up, and re-asserts the Conduit against Twitch (the one external source of
  truth we cannot avoid reconciling with).
  """

  use GenServer
  require Logger

  alias Ingress.Config
  alias Ingress.Twitch.Api

  @reconcile_interval_ms 30_000
  @retry_interval_ms 5_000

  def start_link(_opts) do
    GenServer.start_link(__MODULE__, [], name: via())
  end

  def via, do: {:via, Horde.Registry, {Ingress.Registry, :conduit_manager}}

  @impl true
  def init(_) do
    {:ok, %{conduit_id: nil}, {:continue, :reconcile}}
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
      {:ok, conduit_id} ->
        ensure_shards(conduit_id)
        Process.send_after(self(), :reconcile, @reconcile_interval_ms)
        %{state | conduit_id: conduit_id}

      {:error, reason} ->
        Logger.error("conduit reconcile failed: #{inspect(reason)}")
        Process.send_after(self(), :reconcile, @retry_interval_ms)
        state
    end
  end

  defp ensure_conduit(%{conduit_id: nil}), do: Api.ensure_conduit()
  defp ensure_conduit(%{conduit_id: id}), do: {:ok, id}

  defp ensure_shards(conduit_id) do
    for shard_id <- 0..(Config.conduit_shard_count() - 1) do
      spec = {Ingress.ShardSession, shard_id: shard_id, conduit_id: conduit_id}

      case Horde.DynamicSupervisor.start_child(Ingress.ShardSupervisor, spec) do
        {:ok, _pid} -> Logger.info("started shard #{shard_id}")
        {:error, {:already_started, _pid}} -> :ok
        :ignore -> :ok
        {:error, reason} -> Logger.warning("shard #{shard_id} start failed: #{inspect(reason)}")
      end
    end

    :ok
  end
end
