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

  ## Socket rating vs. the shared NATS budget

  Two capacities gate the pipeline and they are NOT the same number, so the
  dashboard shows them independently:

    * The websocket read+decode+enqueue path is rated far above
      `@shard_rated_eps` — benchmarks put one shard's decode/dispatch near
      12,500 ev/s, with a 10,000 ev/s (80%) operational target. That is a
      *per-shard* ceiling that grows with shard count.
    * JetStream publish is a *shared* ceiling: every shard and pod converges on
      the same broker, so adding shards does not add publish capacity. Its safe
      aggregate is measured per deployment.

  `@shard_rated_eps` stays deliberately low here. Raising it to the 12,500
  socket ceiling would make the autoscaler run fewer, hotter shards on the
  assumption that shard count buys end-to-end capacity — false while NATS is the
  bottleneck. It is only safe to raise after a sustained 50k/60s acceptance test
  passes (0 publish errors, PubAck p95 < 20 ms, consumer lag < 1 s), once the
  async publisher (`Ingress.Nats.Publisher`) and memory-backed streams have
  lifted the shared ceiling. Until then the socket rating and the NATS budget
  are surfaced for observability only, not fed into the shard-count math.
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
  # A shard is "concentrated" (one hot broadcaster, not routine unevenness)
  # when its load is at least this many times the fleet average...
  @concentration_ratio 3
  # ...AND at least this percent of one shard's own rated budget. The ratio
  # alone is noisy on a small/lightly loaded fleet (e.g. one idle shard makes
  # any nonzero shard "infinitely" above average); the absolute floor keeps
  # that case from alerting on routine traffic.
  @concentration_min_pct 25

  def shard_rated_eps, do: @shard_rated_eps
  def target_utilization_pct, do: @target_utilization_pct
  def scale_up_ticks, do: @scale_up_ticks
  def scale_down_ticks, do: @scale_down_ticks
  def concentration_ratio, do: @concentration_ratio
  def concentration_min_pct, do: @concentration_min_pct

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
  Reduces per-shard `{shard_id, {:ok, load} | :error}` results into the sample
  map the rest of this module consumes, tracking which shard carried the
  highest load alongside the existing aggregate/average.

  `expected` is the shard count the caller asked for (independent of how many
  actually responded), preserved so the returned shape is always the same
  regardless of how many results came back.
  """
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

  @doc """
  True when one shard's load looks like a hot broadcaster concentrated on it
  rather than routine unevenness: at least `@concentration_ratio`× the fleet
  average, AND at least `@concentration_min_pct` of one shard's own rated
  budget. Requires more than one responsive shard — "average" is meaningless
  with just one data point, and a lone shard being the max is not
  concentration, it's the whole fleet.

  This never changes a scaling decision (see `evaluate/5`): more shards can't
  move an already-placed hot broadcaster off the shard Twitch assigned it to.
  It only flags that the situation exists so it can be surfaced.
  """
  @spec concentrated?(map()) :: boolean()
  def concentrated?(sample) do
    sample.responsive_count > 1 and
      sample.max_load > sample.avg_load * @concentration_ratio and
      sample.max_load > div(budget_per_window() * @concentration_min_pct, 100)
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
