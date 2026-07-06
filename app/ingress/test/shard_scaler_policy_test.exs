defmodule Ingress.ShardScaler.PolicyTest do
  use ExUnit.Case, async: true

  alias Ingress.ShardScaler.Policy

  @min 3
  @max 20

  defp sample(aggregate, opts \\ []) do
    responsive = Keyword.get(opts, :responsive, 4)
    missing = Keyword.get(opts, :missing, 0)

    %{
      expected_count: responsive + missing,
      responsive_count: responsive,
      missing_count: missing,
      aggregate_load: aggregate,
      avg_load: div(aggregate, max(responsive, 1))
    }
  end

  describe "capacity model" do
    test "budget keeps at least 20% cushion below the rating" do
      assert Policy.budget_per_window() <= div(Policy.rated_per_window() * 80, 100)
    end

    test "shards_needed sizes from aggregate at target utilization" do
      budget = Policy.budget_per_window()
      assert Policy.shards_needed(0) == 1
      assert Policy.shards_needed(budget) == 1
      assert Policy.shards_needed(budget + 1) == 2
      assert Policy.shards_needed(budget * 5) == 5
    end
  end

  describe "scale up" do
    test "trivial traffic never scales up (4 ev/s on one shard, rest idle)" do
      # The original bug: 240 events/window aggregate on a 4-shard fleet
      # must not add capacity.
      s = sample(240)
      assert {4, %{low: 1, high: 0}, :hold} = Policy.evaluate(s, 4, Policy.reset_ticks(), @min, @max)
    end

    test "load within the current fleet's budget holds" do
      s = sample(Policy.budget_per_window() * 4)
      assert {4, %{low: 0, high: 0}, :hold} = Policy.evaluate(s, 4, Policy.reset_ticks(), @min, @max)
    end

    test "a single undercapacity tick does not scale up" do
      s = sample(Policy.budget_per_window() * 4 + 1)
      assert {4, %{high: 1, low: 0}, :hold} = Policy.evaluate(s, 4, Policy.reset_ticks(), @min, @max)
    end

    test "sustained undercapacity jumps straight to the needed count" do
      # 9.6 shards' worth of budgeted traffic on an 8-shard fleet: above
      # budget but below the full rating, so it takes the streak, then the
      # target jumps multi-step to 10, not 9.
      s = sample(div(Policy.budget_per_window() * 96, 10), responsive: 8)
      {8, ticks, :hold} = Policy.evaluate(s, 8, Policy.reset_ticks(), @min, @max)
      assert {10, %{low: 0, high: 0}, :up} = Policy.evaluate(s, 8, ticks, @min, @max)
    end

    test "aggregate beyond the fleet's full rating scales immediately" do
      aggregate = 4 * Policy.rated_per_window() + 1

      assert {needed, %{low: 0, high: 0}, :up} =
               Policy.evaluate(sample(aggregate), 4, Policy.reset_ticks(), @min, @max)

      assert needed == Policy.shards_needed(aggregate)
      assert needed > 4
    end

    test "a within-budget tick between two undercapacity ticks resets the streak" do
      high = sample(Policy.budget_per_window() * 5)
      ok = sample(Policy.budget_per_window() * 4)

      {4, ticks, :hold} = Policy.evaluate(high, 4, Policy.reset_ticks(), @min, @max)
      {4, ticks, :hold} = Policy.evaluate(ok, 4, ticks, @min, @max)
      assert ticks == %{low: 0, high: 0}
      assert {4, %{high: 1, low: 0}, :hold} = Policy.evaluate(high, 4, ticks, @min, @max)
    end

    test "needed count clamps at max_shards" do
      s = sample(Policy.budget_per_window() * 50)
      assert {@max, _, :up} = Policy.evaluate(s, 4, Policy.reset_ticks(), @min, @max)
    end
  end

  describe "scale down" do
    test "requires consecutive overcapacity ticks and drains one shard" do
      s = sample(240, responsive: 8)
      {8, t1, :hold} = Policy.evaluate(s, 8, Policy.reset_ticks(), @min, @max)
      {8, t2, :hold} = Policy.evaluate(s, 8, t1, @min, @max)
      assert {7, %{low: 0, high: 0}, :down} = Policy.evaluate(s, 8, t2, @min, @max)
    end

    test "incomplete sample resets the streak and holds" do
      low = sample(240, responsive: 8)
      {8, ticks, :hold} = Policy.evaluate(low, 8, Policy.reset_ticks(), @min, @max)

      partial = sample(240, responsive: 7, missing: 1)
      assert {8, %{low: 0, high: 0}, :hold} = Policy.evaluate(partial, 8, ticks, @min, @max)
    end

    test "never drops below min_shards" do
      s = sample(0, responsive: @min)
      {_, t1, :hold} = Policy.evaluate(s, @min, Policy.reset_ticks(), @min, @max)
      {_, t2, :hold} = Policy.evaluate(s, @min, t1, @min, @max)
      {_, t3, :hold} = Policy.evaluate(s, @min, t2, @min, @max)
      assert t3 == %{low: 0, high: 0}
    end
  end

  describe "no responsive shards" do
    test "holds and resets ticks" do
      s = sample(0, responsive: 0, missing: 4)

      assert {5, %{low: 0, high: 0}, :hold} =
               Policy.evaluate(s, 5, %{low: 2, high: 0}, @min, @max)
    end
  end
end
