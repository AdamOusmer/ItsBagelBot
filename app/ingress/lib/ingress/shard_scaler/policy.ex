defmodule Ingress.ShardScaler.Policy do
  @moduledoc """
  Pure logic for autoscaler decisions.

  Hysteresis state is a `%{low: n, high: n}` map of consecutive ticks spent
  below/above the thresholds; at most one side is non-zero at any time.
  """

  # Notification events per @load_window_ms per shard above which we add a
  # shard (requires @scale_up_ticks consecutive ticks). 600 events over the
  # 60 s window = 10 ev/s per shard, still far below what one shard handles:
  # this is a safety net against sustained load, not a latency optimizer.
  @scale_up_threshold 600
  # Notification events per @load_window_ms per shard below which we consider
  # removing a shard (requires @scale_down_ticks consecutive ticks).
  @scale_down_threshold 60
  # Consecutive high-load ticks required before scaling up. A single 30 s
  # burst (raid, big stream going live) must not add a shard.
  @scale_up_ticks 2
  # Consecutive low-load ticks required before scaling down.
  @scale_down_ticks 3

  def scale_up_threshold, do: @scale_up_threshold
  def scale_down_threshold, do: @scale_down_threshold
  def scale_up_ticks, do: @scale_up_ticks
  def scale_down_ticks, do: @scale_down_ticks

  @doc """
  Returns the initial (reset) hysteresis state.
  """
  def reset_ticks, do: %{low: 0, high: 0}

  @doc """
  Evaluates load and determines the new target and hysteresis state.

  Returns `{new_target, new_ticks, action}` where action is `:up`, `:down`,
  or `:hold`.
  """
  def evaluate(sample, current_target, ticks, min_shards, max_shards) do
    if sample.responsive_count == 0 do
      {current_target, reset_ticks(), :hold}
    else
      avg = sample.avg_load

      cond do
        avg > @scale_up_threshold ->
          high = ticks.high + 1

          if high >= @scale_up_ticks do
            new_target = min(current_target + 1, max_shards)
            {new_target, reset_ticks(), :up}
          else
            {current_target, %{low: 0, high: high}, :hold}
          end

        avg < @scale_down_threshold ->
          if sample.missing_count > 0 do
            # Incomplete sample: reset the scale-down streak and hold.
            {current_target, reset_ticks(), :hold}
          else
            low = ticks.low + 1

            if low >= @scale_down_ticks do
              new_target = max(current_target - 1, min_shards)
              {new_target, reset_ticks(), :down}
            else
              {current_target, %{low: low, high: 0}, :hold}
            end
          end

        true ->
          {current_target, reset_ticks(), :hold}
      end
    end
  end
end
