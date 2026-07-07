defmodule Ingress.ShardScaler.Policy do
  @moduledoc """
  Pure logic for autoscaler decisions.

  ## Capacity model

  Twitch conduits load-balance notifications across all enabled shards, so
  aggregate load — not any single shard's — is the signal that matters: the
  target is `ceil(aggregate / usable capacity per shard)`, sized so shards
  run at `@target_utilization_pct` of their rating and keep the rest as
  burst cushion.

  A shard's per-event work is deliberately thin: JSON-decode the frame and
  enqueue into `Ingress.Dispatcher` (filtering, re-encode, and NATS publish
  happen in the dispatcher's worker pool). That is tens of microseconds per
  event, >10k ev/s on paper. `@shard_rated_eps` is rated far below that so
  the websocket read loop never falls behind Twitch even with GC, sidecars,
  and noisy neighbours on the small fleet cores.

  Hysteresis state is a `%{low: n, high: n}` map of consecutive ticks spent
  needing more/fewer shards; at most one side is non-zero at any time.
  """

  # Sustained events/second one shard is rated for. Conservative ~5-10% of
  # the theoretical decode+enqueue throughput.
  @shard_rated_eps 1_000
  # Fraction of the rating we aim to use; the remainder (≥20%) is standing
  # cushion for spam bursts between autoscale ticks.
  @target_utilization_pct 80
  # Load samples count notifications over this rolling window (must match
  # the `Ingress.LoadCounter` default used by `Ingress.ShardSession`).
  @load_window_seconds 60
  # Consecutive ticks of undercapacity required before scaling up — unless
  # aggregate load exceeds the fleet's full rating, which scales immediately.
  @scale_up_ticks 2
  # Consecutive ticks of overcapacity required before scaling down.
  @scale_down_ticks 3

  def shard_rated_eps, do: @shard_rated_eps
  def target_utilization_pct, do: @target_utilization_pct
  def scale_up_ticks, do: @scale_up_ticks
  def scale_down_ticks, do: @scale_down_ticks

  @doc """
  Events per window one shard is rated for (100% utilization).
  """
  def rated_per_window, do: @shard_rated_eps * @load_window_seconds

  @doc """
  Events per window we budget per shard when sizing the fleet
  (rating × target utilization).
  """
  def budget_per_window, do: div(rated_per_window() * @target_utilization_pct, 100)

  @doc """
  Shards needed to serve `aggregate_load` (events/window) at the target
  utilization. At least 1.
  """
  def shards_needed(aggregate_load) do
    max(ceil(aggregate_load / budget_per_window()), 1)
  end

  @doc """
  Returns the initial (reset) hysteresis state.
  """
  def reset_ticks, do: %{low: 0, high: 0}

  @doc """
  Evaluates load and determines the new target and hysteresis state.

  Returns `{new_target, new_ticks, action}` where action is `:up`, `:down`,
  or `:hold`.

  Scale-up jumps straight to the computed shard count (multi-step) once the
  need persists for `@scale_up_ticks` ticks, or immediately when aggregate
  load exceeds the current fleet's full rating. Scale-down drains one shard
  per decision after `@scale_down_ticks` ticks, so a lull never mass-removes
  capacity that a returning spike would need back.
  """
  def evaluate(sample, current_target, ticks, min_shards, max_shards) do
    if sample.responsive_count == 0 do
      {current_target, reset_ticks(), :hold}
    else
      # Unresponsive shards contribute no load to the sample, but Twitch only
      # routes to healthy shards, so the responsive aggregate is the real
      # traffic — no extrapolation needed on the way up.
      needed = clamp(shards_needed(sample.aggregate_load), min_shards, max_shards)

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

  defp clamp(value, min_v, max_v), do: value |> max(min_v) |> min(max_v)
end
