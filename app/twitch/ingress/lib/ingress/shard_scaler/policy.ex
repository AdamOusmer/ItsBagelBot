# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ShardScaler.Policy do
  alias Ingress.Capacity

  @scale_up_ticks 2
  @scale_down_ticks 3
  @concentration_ratio 3
  @concentration_min_pct 25

  def shard_rated_eps, do: Capacity.websocket_rated_eps()
  def target_utilization_pct, do: Capacity.target_utilization_pct()
  def scale_up_ticks, do: @scale_up_ticks
  def scale_down_ticks, do: @scale_down_ticks
  def concentration_ratio, do: @concentration_ratio
  def concentration_min_pct, do: @concentration_min_pct

  def autoscale_max_shards(configured_max) do
    min(configured_max, Capacity.websocket_autoscale_max_shards())
  end

  def rated_per_window, do: shard_rated_eps() * Capacity.load_window_seconds()

  def budget_per_window, do: div(rated_per_window() * target_utilization_pct(), 100)

  def shards_needed(aggregate_load) do
    max(ceil(aggregate_load / budget_per_window()), 1)
  end

  @spec summarize_sample(non_neg_integer(), [{term(), {:ok, non_neg_integer()} | :error}]) ::
          map()
  def summarize_sample(expected, per_shard_results) do
    {responsive, total_load, max_load, max_load_shard_id} =
      Enum.reduce(per_shard_results, {0, 0, 0, nil}, fn
        {shard_id, {:ok, load}}, {r, l, max_l, max_id} ->
          if load > max_l do
            {r + 1, l + load, load, shard_id}
          else
            {r + 1, l + load, max_l, max_id}
          end

        {_shard_id, :error}, acc ->
          acc
      end)

    missing = expected - responsive
    avg = if responsive > 0, do: div(total_load, responsive), else: 0

    %{
      expected_count: expected,
      responsive_count: responsive,
      missing_count: missing,
      aggregate_load: total_load,
      avg_load: avg,
      max_load: max_load,
      max_load_shard_id: max_load_shard_id
    }
  end

  @spec concentrated?(map()) :: boolean()
  def concentrated?(sample) do
    sample.responsive_count > 1 and
      sample.max_load > sample.avg_load * @concentration_ratio and
      sample.max_load > div(budget_per_window() * @concentration_min_pct, 100)
  end

  def reset_ticks, do: %{low: 0, high: 0}

  def evaluate(sample, current_target, ticks, min_shards, max_shards) do
    if sample.responsive_count == 0 do
      {current_target, reset_ticks(), :hold}
    else
      effective_max = max(min_shards, autoscale_max_shards(max_shards))
      needed = clamp(shards_needed(sample.aggregate_load), min_shards, effective_max)

      cond do
        needed > current_target ->
          overloaded? = sample.aggregate_load > current_target * rated_per_window()
          high = ticks.high + 1

          if overloaded? or high >= @scale_up_ticks do
            {needed, reset_ticks(), :up}
          else
            {current_target, %{low: 0, high: high}, :hold}
          end

        needed < current_target ->
          if sample.missing_count > 0 do
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

  defp clamp(value, min_v, max_v), do: value |> max(min_v) |> min(max_v)
end
