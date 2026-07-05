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

  When `autoscale: true` the scaler samples the aggregate per-shard load
  (notifications received in the last 60 s per shard, collected from
  `Ingress.ShardSession.status/2`) and adjusts the target:

    * avg load per shard > `@scale_up_threshold`   → increment target by 1
    * avg load per shard < `@scale_down_threshold`  → decrement target by 1
    * otherwise                                      → no change

  The effective desired count is always `clamp(target, min_shards, max_shards)`.
  Decisions run every `@autoscale_interval_ms`. Scale-down requires the low
  watermark to be sustained for `@scale_down_ticks` consecutive ticks to
  avoid flapping.

  ## Thresholds (tunable via module attributes)

    * `@scale_up_threshold`   = 50 events/window per shard   (~0.83 ev/s)
    * `@scale_down_threshold` = 10 events/window per shard   (~0.17 ev/s)

  These are deliberately conservative: a single shard can comfortably handle
  far more traffic; the autoscaler is a safety net against sustained load
  spikes, not a latency optimizer.
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
    {:ok, %{target: target, autoscale: true, low_ticks: 0, last_sample: nil}}
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
    {:reply, :ok, %{state | target: clamped, low_ticks: 0}}
  end

  @impl true
  def handle_call({:set_autoscale, enabled}, _from, state) do
    Logger.info("shard_scaler: autoscale #{state.autoscale} → #{enabled}")
    {:reply, :ok, %{state | autoscale: enabled, low_ticks: 0}}
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

    {new_target, low_ticks, action} =
      Ingress.ShardScaler.Policy.evaluate(
        sample,
        state.target,
        state.low_ticks,
        min_s,
        Config.max_shards()
      )

    case action do
      :up ->
        Logger.info(
          "shard_scaler: autoscale up avg_load=#{sample.avg_load} → target #{state.target} → #{new_target}"
        )

      :down ->
        Logger.info(
          "shard_scaler: autoscale down avg_load=#{sample.avg_load} ticks=#{low_ticks} → target #{state.target} → #{new_target}"
        )

      :hold ->
        if low_ticks > 0 do
          Logger.debug("shard_scaler: low load avg=#{sample.avg_load} (#{low_ticks}/3 ticks)")
        end
    end

    %{state | target: new_target, low_ticks: low_ticks}
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
