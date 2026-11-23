# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.
defmodule Platform.BackoffTest do
  use ExUnit.Case, async: true

  alias Platform.Backoff

  @samples 200

  test "jittered_ms doubles per attempt, caps, and adds up to one second" do
    cases = [
      {1, 500, 15_000, 6, 500},
      {2, 500, 15_000, 6, 1_000},
      {4, 500, 15_000, 6, 4_000},
      {10, 500, 15_000, 6, 15_000},
      {10, 500, 1_000_000, 3, 4_000}
    ]

    for {attempts, base, max, cap, floor} <- cases, _ <- 1..@samples do
      delay = Backoff.jittered_ms(attempts, base, max, cap)
      assert delay > floor and delay <= floor + 1_000
    end
  end

  test "uniform_ms and between_ms stay inside their bounds" do
    cases = [
      {fn -> Backoff.uniform_ms(5) end, 1..5},
      {fn -> Backoff.uniform_ms(1) end, 1..1},
      {fn -> Backoff.between_ms(500, 1_000) end, 500..1_000},
      {fn -> Backoff.between_ms(7, 7) end, 7..7}
    ]

    for {draw, range} <- cases, _ <- 1..@samples do
      assert draw.() in range
    end
  end

  test "between_ms rejects an inverted range" do
    assert_raise FunctionClauseError, fn -> Backoff.between_ms(10, 5) end
  end
end
