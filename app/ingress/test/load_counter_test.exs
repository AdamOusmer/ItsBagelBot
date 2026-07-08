defmodule Ingress.LoadCounterTest do
  use ExUnit.Case, async: true

  alias Ingress.LoadCounter

  # BEAM monotonic time is a large negative number. Every test that models
  # production behaviour must use timestamps in that range: the original bug
  # (a frozen, ever-growing load reading on the admin dashboard) only
  # reproduced with negative timestamps and passed with positive ones.
  @beam_mono_ms -576_460_751_000_000

  describe "with real (negative) monotonic timestamps" do
    test "window expires after traffic stops" do
      base = @beam_mono_ms
      counter = LoadCounter.new()

      counter =
        Enum.reduce(1..900, counter, fn i, acc ->
          LoadCounter.increment(acc, base + i * 100)
        end)

      # 900 events over 90s: only the last 60s may remain in the window.
      {during, counter} = LoadCounter.value(counter, base + 90_000)
      assert during <= 600
      assert during > 0

      # Hours later with no traffic the reading must be zero, not frozen.
      {idle, _} = LoadCounter.value(counter, base + 90_000 + 3 * 3600 * 1000)
      assert idle == 0
    end

    test "live System.monotonic_time values count and expire" do
      now = System.monotonic_time(:millisecond)
      counter = LoadCounter.new()

      counter = Enum.reduce(1..5, counter, fn _, acc -> LoadCounter.increment(acc, now) end)

      {value, counter} = LoadCounter.value(counter, now)
      assert value == 5

      {expired, _} = LoadCounter.value(counter, now + 61_000)
      assert expired == 0
    end
  end

  describe "windowing" do
    test "counts only events inside the window" do
      counter = LoadCounter.new(60)
      counter = LoadCounter.increment(counter, 0)
      counter = LoadCounter.increment(counter, 30_000)
      counter = LoadCounter.increment(counter, 59_000)

      # At t=61s the t=0 event has left the 60s window.
      {value, _} = LoadCounter.value(counter, 61_000)
      assert value == 2
    end

    test "same-second increments accumulate in one bucket" do
      counter = LoadCounter.new()
      counter = Enum.reduce(1..10, counter, fn _, acc -> LoadCounter.increment(acc, 5_500) end)

      assert counter.current_count == 10
      assert counter.completed_buckets == []

      {value, _} = LoadCounter.value(counter, 5_900)
      assert value == 10
    end

    test "long idle gap clears all buckets at once" do
      counter = LoadCounter.new(60)
      counter = Enum.reduce(0..59, counter, fn s, acc -> LoadCounter.increment(acc, s * 1000) end)

      {full, counter} = LoadCounter.value(counter, 59_999)
      assert full == 60

      {after_gap, counter} = LoadCounter.value(counter, 59_999 + 600_000)
      assert after_gap == 0
      assert counter.completed_buckets == []
      assert counter.completed_total == 0
    end
  end

  describe "clock regression" do
    test "an older timestamp neither crashes nor inflates the total" do
      counter = LoadCounter.new()
      counter = LoadCounter.increment(counter, 10_000)
      counter = LoadCounter.increment(counter, 9_000)

      {value, _} = LoadCounter.value(counter, 10_500)
      assert value == 2
    end

    test "value with an older timestamp keeps the window anchored" do
      counter = LoadCounter.new()
      counter = LoadCounter.increment(counter, 10_000)

      {value, _} = LoadCounter.value(counter, 8_000)
      assert value == 1
    end
  end

  test "fresh counter reads zero" do
    {value, _} = LoadCounter.value(LoadCounter.new(), System.monotonic_time(:millisecond))
    assert value == 0
  end
end
