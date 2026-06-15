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
  alias Ingress.Twitch.Api

  # --- tunables ---------------------------------------------------------------

  # Notification events per @load_window_ms per shard above which we add a shard.
  @scale_up_threshold 50
  # Notification events per @load_window_ms per shard below which we consider
  # removing a shard (requires @scale_down_ticks consecutive ticks).
  @scale_down_threshold 10
  # How often the autoscaler evaluates load.
  @autoscale_interval_ms 30_000
  # Consecutive low-load ticks required before scaling down (hysteresis).
  @scale_down_ticks 3
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
    # Seed target from the live conduit's shard_count so we do not race the
    # ConduitManager on boot with a stale env value.
    target = seed_target()
    Logger.info("shard_scaler started: target=#{target} on #{node()}")
    schedule_autoscale()
    {:ok, %{target: target, autoscale: false, low_ticks: 0}}
  end

  @impl true
  def handle_call(:desired, _from, state) do
    {:reply, compute_desired(state), state}
  end

  @impl true
  def handle_call(:status, _from, state) do
    load = aggregate_load(state)
    min_s = min_shards()

    {:reply,
     %{
       target: state.target,
       autoscale: state.autoscale,
       min_shards: min_s,
       desired: compute_desired(state),
       load: load
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

  # Seed from the live conduit if reachable; fall back to the env value so
  # the first reconcile does not needlessly resize.
  defp seed_target do
    case Api.list_conduits() do
      {:ok, [%{"shard_count" => n} | _]} when is_integer(n) and n > 0 ->
        Logger.info("shard_scaler: seeded target from conduit shard_count=#{n}")
        n

      _ ->
        Config.conduit_shard_count()
    end
  end

  # min_shards: one per node currently visible in the cluster (self + peers).
  defp min_shards do
    self_node = node()
    length([self_node | Node.list()])
  end

  defp compute_desired(%{target: target, autoscale: false}) do
    max(target, min_shards())
  end

  defp compute_desired(%{target: target, autoscale: true} = state) do
    load = aggregate_load(state)
    min_s = min_shards()
    lower = max(target, min_s)
    clamp(load_target(load, compute_desired_raw(state)), lower, Config.max_shards())
  end

  # Raw desired without clamping — used internally by load_target to know
  # the current scale to decide whether to go up or down.
  defp compute_desired_raw(%{target: target}) do
    max(target, min_shards())
  end

  # Given aggregate load across all shards and the current shard count,
  # propose a new count.
  defp load_target(load, current) when current == 0, do: max(load, 1)

  defp load_target(load, current) do
    avg = div(load, current)

    cond do
      avg > @scale_up_threshold -> current + 1
      avg < @scale_down_threshold -> max(current - 1, 1)
      true -> current
    end
  end

  defp evaluate_autoscale(state) do
    load = aggregate_load(state)
    current = compute_desired_raw(state)
    avg = if current > 0, do: div(load, current), else: 0

    cond do
      avg > @scale_up_threshold ->
        new_target = min(state.target + 1, Config.max_shards())
        Logger.info("shard_scaler: autoscale up avg_load=#{avg} → target #{state.target} → #{new_target}")
        %{state | target: new_target, low_ticks: 0}

      avg < @scale_down_threshold ->
        ticks = state.low_ticks + 1

        if ticks >= @scale_down_ticks do
          new_target = max(state.target - 1, min_shards())
          Logger.info("shard_scaler: autoscale down avg_load=#{avg} ticks=#{ticks} → target #{state.target} → #{new_target}")
          %{state | target: new_target, low_ticks: 0}
        else
          Logger.debug("shard_scaler: low load avg=#{avg} (#{ticks}/#{@scale_down_ticks} ticks)")
          %{state | low_ticks: ticks}
        end

      true ->
        %{state | low_ticks: 0}
    end
  end

  # Collect the load field from every currently-registered shard and sum.
  # Unresponsive shards contribute 0 (conservative: do not scale down when
  # a shard is mid-reconnect and we cannot reach it).
  defp aggregate_load(_state) do
    current = compute_desired_raw(%{target: 0})

    0..(current - 1)
    |> Enum.map(fn shard_id ->
      case Horde.Registry.lookup(Ingress.Registry, {:shard, shard_id}) do
        [{pid, _}] ->
          try do
            status = Ingress.ShardSession.status(pid, 1_000)
            Map.get(status, :load, 0)
          catch
            :exit, _ -> 0
          end

        [] ->
          0
      end
    end)
    |> Enum.sum()
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
      load: 0
    }
  end
end
