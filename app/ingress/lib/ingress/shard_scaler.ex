defmodule Ingress.ShardScaler do
  @moduledoc """
  Cluster-singleton that owns the desired shard count for the Conduit.

  Exactly one instance runs in the BEAM cluster (registered in
  `Ingress.Registry`, supervised by `Ingress.ShardSupervisor`, started by
  `Ingress.Bootstrapper`). The same Horde failover that protects the
  `Ingress.ConduitManager` protects this process.

  ## Manual floor

  The `target` is the operator-chosen minimum. It is never allowed to fall
  below `min_shards` (one per cluster node), so the floor adjusts
  automatically as nodes join and leave.

  ## Autoscaler

  When `autoscale: true` the scaler samples the aggregate load across shards
  (notifications received in the last 60 s, collected from
  `Ingress.ShardSession.status/2`) and sizes the fleet from capacity:
  Twitch conduits load-balance notifications across all enabled shards, so
  the needed count is `ceil(aggregate / per-shard budget)` where the budget
  is the shard rating × target utilization (≥20% kept as burst cushion).

    * needed > target for consecutive ticks → jump target to needed
      (immediately when aggregate exceeds the fleet's full rating)
    * needed < target for consecutive ticks → drain one shard
    * otherwise                             → hold

  The effective desired count is always `clamp(target, min_shards, max_shards)`.
  Decisions run every `@autoscale_interval_ms`. Ratings, utilization, and
  tick counts live in `Ingress.ShardScaler.Policy`.
  """

  use GenServer
  require Logger

  alias Ingress.Config
  # --- tunables ---------------------------------------------------------------

  # How often the autoscaler evaluates load.
  @autoscale_interval_ms 30_000
  # --- public API ------------------------------------------------------------

  def start_link(_opts) do
    GenServer.start_link(__MODULE__, [], name: via())
  end

  def via, do: {:via, Horde.Registry, {Ingress.Registry, :shard_scaler}}

  @doc """
  Returns the effective desired shard count for this instant.

  `min_shards` = one per BEAM node currently in the cluster.

  When `autoscale` is off: `max(target, min_shards)`.
  When `autoscale` is on:  `clamp(load_target, max(target, min_shards), max_shards)`.
  """
  @spec desired() :: non_neg_integer()
  def desired do
    case Horde.Registry.lookup(Ingress.Registry, :shard_scaler) do
      [{pid, _}] ->
        try do
          GenServer.call(pid, :desired, 2_000)
        catch
          :exit, _ -> Config.conduit_shard_count()
        end

      [] ->
        Config.conduit_shard_count()
    end
  end

  @doc """
  Set the manual target floor. Clamped to `[min_shards, max_shards]`.
  """
  @spec set_target(non_neg_integer()) :: :ok | {:error, :not_running}
  def set_target(count) when is_integer(count) and count >= 0 do
    call_singleton({:set_target, count})
  end

  @doc """
  Enable or disable the load-based autoscaler.
  """
  @spec set_autoscale(boolean()) :: :ok | {:error, :not_running}
  def set_autoscale(enabled) when is_boolean(enabled) do
    call_singleton({:set_autoscale, enabled})
  end

  @doc """
  Returns the full scaler status map, used by `Ingress.AdminRpc.snapshot/0`.
  """
  @spec status() :: map()
  def status do
    case Horde.Registry.lookup(Ingress.Registry, :shard_scaler) do
      [{pid, _}] ->
        try do
          GenServer.call(pid, :status, 2_000)
        catch
          :exit, _ -> fallback_status()
        end

      [] ->
        fallback_status()
    end
  end

  # --- GenServer callbacks ---------------------------------------------------

  @impl true
  def init(_) do
    # The scaler is the source of truth. ConduitManager performs the one
    # startup read needed to locate the pinned conduit and applies this target
    # only when Twitch's recorded count differs.
    target = Config.conduit_shard_count()
    Logger.info("shard_scaler started: target=#{target} on #{node()}")
    schedule_autoscale()

    {:ok,
     %{
       target: target,
       autoscale: true,
       ticks: Ingress.ShardScaler.Policy.reset_ticks(),
       last_sample: nil
     }}
  end

  @impl true
  def handle_call(:desired, _from, state) do
    {:reply, compute_desired(state), state}
  end

  @impl true
  def handle_call(:status, _from, state) do
    min_s = min_shards()
    load = if state.last_sample, do: state.last_sample.aggregate_load, else: 0

    {:reply,
     %{
       target: state.target,
       autoscale: state.autoscale,
       min_shards: min_s,
       desired: compute_desired(state),
       load: load,
       last_sample: state.last_sample
     }, state}
  end

  @impl true
  def handle_call({:set_target, count}, _from, state) do
    clamped = clamp(count, min_shards(), Config.max_shards())
    Logger.info("shard_scaler: manual target #{state.target} → #{clamped}")
    {:reply, :ok, %{state | target: clamped, ticks: Ingress.ShardScaler.Policy.reset_ticks()}}
  end

  @impl true
  def handle_call({:set_autoscale, enabled}, _from, state) do
    Logger.info("shard_scaler: autoscale #{state.autoscale} → #{enabled}")
    {:reply, :ok, %{state | autoscale: enabled, ticks: Ingress.ShardScaler.Policy.reset_ticks()}}
  end

  @impl true
  def handle_info(:autoscale_tick, %{autoscale: false} = state) do
    schedule_autoscale()
    {:noreply, state}
  end

  @impl true
  def handle_info(:autoscale_tick, state) do
    state = evaluate_autoscale(state)
    schedule_autoscale()
    {:noreply, state}
  end

  # --- private ---------------------------------------------------------------

  defp call_singleton(msg) do
    case Horde.Registry.lookup(Ingress.Registry, :shard_scaler) do
      [{pid, _}] ->
        try do
          GenServer.call(pid, msg, 2_000)
        catch
          :exit, _ -> {:error, :not_running}
        end

      [] ->
        {:error, :not_running}
    end
  end

  # min_shards: one per node currently visible in the cluster (self + peers).
  defp min_shards do
    self_node = node()
    length([self_node | Node.list()])
  end

  defp compute_desired(state) do
    clamp(state.target, min_shards(), Config.max_shards())
  end

  defp evaluate_autoscale(state) do
    sample = sample_shards(state)
    state = %{state | last_sample: sample}

    min_s = min_shards()

    {new_target, ticks, action} =
      Ingress.ShardScaler.Policy.evaluate(
        sample,
        state.target,
        state.ticks,
        min_s,
        Config.max_shards()
      )

    case action do
      :up ->
        Logger.info(
          "shard_scaler: autoscale up load=#{sample.aggregate_load}/window → target #{state.target} → #{new_target}"
        )

      :down ->
        Logger.info(
          "shard_scaler: autoscale down load=#{sample.aggregate_load}/window → target #{state.target} → #{new_target}"
        )

      :hold ->
        cond do
          ticks.high > 0 ->
            Logger.debug(
              "shard_scaler: undercapacity load=#{sample.aggregate_load}/window (#{ticks.high}/#{Ingress.ShardScaler.Policy.scale_up_ticks()} ticks)"
            )

          ticks.low > 0 ->
            Logger.debug(
              "shard_scaler: overcapacity load=#{sample.aggregate_load}/window (#{ticks.low}/#{Ingress.ShardScaler.Policy.scale_down_ticks()} ticks)"
            )

          true ->
            :ok
        end
    end

    %{state | target: new_target, ticks: ticks}
  end

  defp sample_shards(state) do
    expected = compute_desired(state)

    if expected == 0 do
      %{expected_count: 0, responsive_count: 0, missing_count: 0, aggregate_load: 0, avg_load: 0}
    else
      results =
        0..(expected - 1)
        |> Task.async_stream(
          fn shard_id ->
            case Horde.Registry.lookup(Ingress.Registry, {:shard, shard_id}) do
              [{pid, _}] ->
                try do
                  status = Ingress.ShardSession.status(pid, 1_000)
                  {:ok, Map.get(status, :load, 0)}
                catch
                  :exit, _ -> :error
                end

              [] ->
                :error
            end
          end,
          max_concurrency: 8,
          timeout: 1_500,
          on_timeout: :kill_task
        )
        |> Enum.to_list()

      {responsive, total_load} =
        Enum.reduce(results, {0, 0}, fn
          {:ok, {:ok, load}}, {r, l} -> {r + 1, l + load}
          _, {r, l} -> {r, l}
        end)

      missing = expected - responsive
      avg = if responsive > 0, do: div(total_load, responsive), else: 0

      %{
        expected_count: expected,
        responsive_count: responsive,
        missing_count: missing,
        aggregate_load: total_load,
        avg_load: avg
      }
    end
  end

  defp schedule_autoscale do
    Process.send_after(self(), :autoscale_tick, @autoscale_interval_ms)
  end

  defp clamp(value, min_v, max_v), do: value |> max(min_v) |> min(max_v)

  # Status returned when the singleton is unreachable: callers get honest
  # defaults instead of an exception.
  defp fallback_status do
    %{
      target: Config.conduit_shard_count(),
      autoscale: false,
      min_shards: min_shards(),
      desired: Config.conduit_shard_count(),
      load: 0,
      last_sample: nil
    }
  end
end
