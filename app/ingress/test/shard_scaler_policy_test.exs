defmodule Ingress.ShardScaler.PolicyTest do
  use ExUnit.Case, async: true

  alias Ingress.ShardScaler.Policy

  @min 3
  @max 10

  defp sample(avg, opts \\ []) do
    responsive = Keyword.get(opts, :responsive, @min)
    missing = Keyword.get(opts, :missing, 0)

    %{
      expected_count: responsive + missing,
      responsive_count: responsive,
      missing_count: missing,
      aggregate_load: avg * max(responsive, 1),
      avg_load: avg
    }
  end

  describe "scale up" do
    test "one hot shard among idle ones stays below the average threshold" do
      # 4 ev/s on a single shard = 240 events/window; the other two shards
      # are idle, so avg = 80. Well under the 600 watermark: hold.
      s = %{sample(80) | aggregate_load: 240}
      assert {3, ticks, :hold} = Policy.evaluate(s, 3, Policy.reset_ticks(), @min, @max)
      assert ticks == %{low: 0, high: 0}
    end

    test "a single high tick does not scale up" do
      s = sample(Policy.scale_up_threshold() + 1)
      assert {3, %{high: 1, low: 0}, :hold} = Policy.evaluate(s, 3, Policy.reset_ticks(), @min, @max)
    end

    test "sustained high load scales up after the required ticks" do
      s = sample(Policy.scale_up_threshold() + 1)
      {3, ticks, :hold} = Policy.evaluate(s, 3, Policy.reset_ticks(), @min, @max)
      assert {4, %{high: 0, low: 0}, :up} = Policy.evaluate(s, 3, ticks, @min, @max)
    end

    test "a normal tick between two high ticks resets the streak" do
      high = sample(Policy.scale_up_threshold() + 1)
      mid = sample(Policy.scale_up_threshold() - 1)

      {3, ticks, :hold} = Policy.evaluate(high, 3, Policy.reset_ticks(), @min, @max)
      {3, ticks, :hold} = Policy.evaluate(mid, 3, ticks, @min, @max)
      assert ticks == %{low: 0, high: 0}
      assert {3, %{high: 1, low: 0}, :hold} = Policy.evaluate(high, 3, ticks, @min, @max)
    end

    test "scale up clamps at max_shards" do
      s = sample(Policy.scale_up_threshold() + 1)
      {_, ticks, :hold} = Policy.evaluate(s, @max, Policy.reset_ticks(), @min, @max)
      assert {@max, _, :up} = Policy.evaluate(s, @max, ticks, @min, @max)
    end
  end

  describe "scale down" do
    test "requires consecutive low ticks" do
      s = sample(Policy.scale_down_threshold() - 1)
      {5, t1, :hold} = Policy.evaluate(s, 5, Policy.reset_ticks(), @min, @max)
      {5, t2, :hold} = Policy.evaluate(s, 5, t1, @min, @max)
      assert {4, %{low: 0, high: 0}, :down} = Policy.evaluate(s, 5, t2, @min, @max)
    end

    test "incomplete sample resets the streak and holds" do
      low = sample(Policy.scale_down_threshold() - 1)
      {5, ticks, :hold} = Policy.evaluate(low, 5, Policy.reset_ticks(), @min, @max)

      partial = sample(Policy.scale_down_threshold() - 1, missing: 1)
      assert {5, %{low: 0, high: 0}, :hold} = Policy.evaluate(partial, 5, ticks, @min, @max)
    end

    test "never drops below min_shards" do
      s = sample(0)
      {_, t1, :hold} = Policy.evaluate(s, @min, Policy.reset_ticks(), @min, @max)
      {_, t2, :hold} = Policy.evaluate(s, @min, t1, @min, @max)
      assert {@min, _, :down} = Policy.evaluate(s, @min, t2, @min, @max)
    end
  end

  describe "no responsive shards" do
    test "holds and resets ticks" do
      s = sample(0, responsive: 0, missing: 3)
      assert {5, %{low: 0, high: 0}, :hold} =
               Policy.evaluate(s, 5, %{low: 2, high: 0}, @min, @max)
    end
  end

  describe "dead band" do
    test "load between thresholds holds and resets both streaks" do
      s = sample(div(Policy.scale_up_threshold() + Policy.scale_down_threshold(), 2))

      assert {5, %{low: 0, high: 0}, :hold} =
               Policy.evaluate(s, 5, %{low: 0, high: 1}, @min, @max)
    end
  end
end
