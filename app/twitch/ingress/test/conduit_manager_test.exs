# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.ConduitManagerTest do
  use ExUnit.Case, async: true

  alias Ingress.ConduitManager

  describe "start_until_free/3" do
    test "takes the successor from the first start that succeeds" do
      pid = self()

      assert {:started, ^pid} =
               ConduitManager.start_until_free(fn -> {:started, pid} end, 1_000, fake_clock())
    end

    test "retries a blocked start until the released name has propagated" do
      pid = self()
      calls = :counters.new(1, [])

      start_fun = fn ->
        :counters.add(calls, 1, 1)

        if :counters.get(calls, 1) < 3, do: :blocked, else: {:started, pid}
      end

      assert {:started, ^pid} = ConduitManager.start_until_free(start_fun, 1_000, fake_clock())
      assert :counters.get(calls, 1) == 3
    end

    test "gives up once the propagation window closes, so the caller can roll back" do
      calls = :counters.new(1, [])

      start_fun = fn ->
        :counters.add(calls, 1, 1)
        :blocked
      end

      assert {:error, :name_release_timeout} =
               ConduitManager.start_until_free(start_fun, 30, fake_clock())

      assert :counters.get(calls, 1) == 7
    end
  end

  defp fake_clock do
    ticks = :counters.new(1, [])

    [
      poll_ms: 5,
      now_fun: fn -> :counters.get(ticks, 1) end,
      sleep_fun: fn ms -> :counters.add(ticks, 1, ms) end
    ]
  end
end
