# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ShardScaler do
  use GenServer
  require Logger

  alias Ingress.Config.Twitch, as: TwitchConfig
  alias Ingress.{Metrics, Singleton}

  @autoscale_interval_ms 30_000
  @call_timeout_ms 2_000
  @shard_timeout_ms 1_000

  def start_link(_opts) do
    GenServer.start_link(__MODULE__, [], name: via())
  end

  def via, do: {:via, Horde.Registry, {Ingress.Registry, :shard_scaler}}

  @spec desired() :: non_neg_integer()
  def desired do
    case fetch_desired() do
      {:ok, count, _pid} -> count
      :error -> TwitchConfig.conduit_shard_count()
    end
  end

  @spec fetch_desired() :: {:ok, non_neg_integer(), pid()} | :error
  def fetch_desired do
    with {:ok, pid} <- Singleton.lookup(:shard_scaler),
         {:ok, count} <- Singleton.safe_call(pid, :desired, @call_timeout_ms) do
      {:ok, count, pid}
    end
  end

  @spec set_target(non_neg_integer()) :: :ok | {:error, :not_running}
  def set_target(count) when is_integer(count) and count >= 0 do
    call_singleton({:set_target, count})
  end

  @spec set_autoscale(boolean()) :: :ok | {:error, :not_running}
  def set_autoscale(enabled) when is_boolean(enabled) do
    call_singleton({:set_autoscale, enabled})
  end

  @spec status() :: map()
  def status do
    Singleton.call(:shard_scaler, :status, @call_timeout_ms, fn _reason -> fallback_status() end)
  end

  @impl true
  def init(_) do
    target = TwitchConfig.conduit_shard_count()
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
    max_load = if state.last_sample, do: state.last_sample.max_load, else: 0
    max_load_shard_id = if state.last_sample, do: state.last_sample.max_load_shard_id, else: nil

    {:reply,
     %{
       target: state.target,
       autoscale: state.autoscale,
       min_shards: min_s,
       max_shards: effective_max_shards(state.autoscale, min_s),
       desired: compute_desired(state),
       load: load,
       max_load: max_load,
       max_load_shard_id: max_load_shard_id,
       last_sample: state.last_sample
     }, state}
  end

  @impl true
  def handle_call({:set_target, count}, _from, state) do
    clamped = clamp(count, min_shards(), TwitchConfig.max_shards())
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

  defp call_singleton(msg) do
    Singleton.call(:shard_scaler, msg, @call_timeout_ms, fn _reason -> {:error, :not_running} end)
  end

  defp min_shards do
    self_node = node()
    length([self_node | Node.list()])
  end

  defp compute_desired(state) do
    min_s = min_shards()
    clamp(state.target, min_s, effective_max_shards(state.autoscale, min_s))
  end

  defp effective_max_shards(true, min_s) do
    max(min_s, Ingress.ShardScaler.Policy.autoscale_max_shards(TwitchConfig.max_shards()))
  end

  defp effective_max_shards(false, _min_s), do: TwitchConfig.max_shards()

  defp evaluate_autoscale(state) do
    sample = sample_shards(state)
    state = %{state | last_sample: sample}
    check_concentration(sample)

    min_s = min_shards()

    current_target = compute_desired(state)

    {new_target, ticks, action} =
      Ingress.ShardScaler.Policy.evaluate(
        sample,
        current_target,
        state.ticks,
        min_s,
        TwitchConfig.max_shards()
      )

    case action do
      :up ->
        Logger.info(
          "shard_scaler: autoscale up load=#{sample.aggregate_load}/window → target #{current_target} → #{new_target}"
        )

      :down ->
        Logger.info(
          "shard_scaler: autoscale down load=#{sample.aggregate_load}/window → target #{current_target} → #{new_target}"
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

  defp check_concentration(sample) do
    if Ingress.ShardScaler.Policy.concentrated?(sample) do
      Logger.warning(
        "shard_scaler: shard #{sample.max_load_shard_id} concentrated at load=#{sample.max_load}/window vs avg=#{sample.avg_load}/window"
      )

      Metrics.event("ShardLoadConcentration", %{
        shard_id: sample.max_load_shard_id,
        max_load: sample.max_load,
        avg_load: sample.avg_load,
        responsive_count: sample.responsive_count
      })
    end
  end

  defp sample_shards(state) do
    expected = compute_desired(state)

    if expected == 0 do
      Ingress.ShardScaler.Policy.summarize_sample(0, [])
    else
      shard_ids = 0..(expected - 1)

      per_shard_results =
        shard_ids
        |> Task.async_stream(
          &sample_shard/1,
          max_concurrency: 8,
          timeout: 1_500,
          on_timeout: :kill_task
        )
        |> Enum.zip(shard_ids)
        |> Enum.map(fn
          {{:ok, result}, shard_id} -> {shard_id, result}
          {{:exit, _reason}, shard_id} -> {shard_id, :error}
        end)

      Ingress.ShardScaler.Policy.summarize_sample(expected, per_shard_results)
    end
  end

  defp sample_shard(shard_id) do
    case Singleton.call({:shard, shard_id}, :status, @shard_timeout_ms, fn _reason -> :error end) do
      %{} = status -> {:ok, Map.get(status, :load, 0)}
      :error -> :error
    end
  end

  defp schedule_autoscale do
    Process.send_after(self(), :autoscale_tick, @autoscale_interval_ms)
  end

  defp clamp(value, min_v, max_v), do: value |> max(min_v) |> min(max_v)

  defp fallback_status do
    %{
      target: TwitchConfig.conduit_shard_count(),
      autoscale: false,
      min_shards: min_shards(),
      max_shards: TwitchConfig.max_shards(),
      desired: TwitchConfig.conduit_shard_count(),
      load: 0,
      max_load: 0,
      max_load_shard_id: nil,
      last_sample: nil
    }
  end
end
