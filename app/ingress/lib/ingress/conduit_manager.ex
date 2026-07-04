defmodule Ingress.ConduitManager do
  @moduledoc """
  Cluster-singleton reconciler for the Conduit.

  Exactly one instance runs in the BEAM cluster (registered in
  `Ingress.Registry`, supervised by `Ingress.ShardSupervisor`, so it fails
  over with the rest of the Horde-managed processes). It ensures the Conduit
  exists on Twitch's side with the desired shard count (sourced from
  `Ingress.ShardScaler`), then converges the running `Ingress.ShardSession`
  processes to match — starting missing shards and stopping excess ones.

  Reconciliation repeats periodically: it heals shards whose supervisor gave
  up, re-asserts the Conduit size against Twitch (the one external source of
  truth we cannot avoid reconciling with), and shrinks when the scaler's
  desired count drops below the current session count.
  """

  use GenServer
  require Logger

  alias Ingress.ShardScaler
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
        desired = ShardScaler.desired()
        converge_shards(conduit_id, desired)
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

  # Converge the Conduit (Twitch side) and local ShardSession processes to
  # exactly `desired` shards. Grows or shrinks as needed.
  #
  # Order of operations:
  #   1. Resize the Conduit on Twitch's side so it accepts exactly `desired`
  #      shard bindings. update_conduit/2 works both directions.
  #   2. Stop sessions for shard IDs >= desired (the Conduit no longer has
  #      slots for them; stopping is safe because :transient restart means a
  #      deliberate shutdown will not restart the session).
  #   3. Start any missing sessions for shard IDs 0..(desired-1).
  defp converge_shards(conduit_id, desired) when desired > 0 do
    case Api.update_conduit(conduit_id, desired) do
      :ok ->
        Logger.debug("conduit resized to #{desired} shards")

      {:error, reason} ->
        Logger.warning("conduit resize to #{desired} failed: #{inspect(reason)}")
    end

    stop_excess_shards(desired)
    start_missing_shards(conduit_id, desired)
  end

  # Stop ShardSession processes for shard IDs >= desired. We look them up in
  # the cluster-wide registry; if they live on a remote node Horde still
  # terminates them via the DynamicSupervisor.
  defp stop_excess_shards(desired) do
    # Discover running shard IDs by scanning the registry for {:shard, _} keys.
    running_ids =
      Horde.Registry.select(Ingress.Registry, [
        {{{:shard, :"$1"}, :_, :_}, [], [:"$1"]}
      ])

    for shard_id <- running_ids, shard_id >= desired do
      case Horde.Registry.lookup(Ingress.Registry, {:shard, shard_id}) do
        [{pid, _}] ->
          Logger.info("stopping excess shard #{shard_id} (desired=#{desired})")

          case Horde.DynamicSupervisor.terminate_child(Ingress.ShardSupervisor, pid) do
            :ok ->
              :ok

            {:error, reason} ->
              Logger.warning("stop shard #{shard_id} failed: #{inspect(reason)}")
          end

        [] ->
          :ok
      end
    end
  end

  defp start_missing_shards(conduit_id, desired) do
    for shard_id <- 0..(desired - 1) do
      spec = {Ingress.ShardSession, shard_id: shard_id, conduit_id: conduit_id}

      case Horde.DynamicSupervisor.start_child(Ingress.ShardSupervisor, spec) do
        {:ok, _pid} -> Logger.info("started shard #{shard_id}")
        {:error, {:already_started, _pid}} -> :ok
        :ignore -> :ok
        {:error, reason} -> Logger.warning("shard #{shard_id} start failed: #{inspect(reason)}")
      end
    end
  end
end
