defmodule Ingress.ConduitManagerTest do
  use ExUnit.Case, async: true

  alias Ingress.ConduitManager

  describe "start_until_free/3" do
    test "takes the successor from the first start that succeeds" do
      pid = self()

      assert {:started, ^pid} =
               ConduitManager.start_until_free(fn -> {:started, pid} end, deadline(1_000), 5)
    end

    test "retries a blocked start until the released name has propagated" do
      pid = self()
      calls = :counters.new(1, [])

      start_fun = fn ->
        :counters.add(calls, 1, 1)

        # The placing node's registry replica still carries the old
        # registration for the first two attempts.
        if :counters.get(calls, 1) < 3, do: :blocked, else: {:started, pid}
      end

      assert {:started, ^pid} = ConduitManager.start_until_free(start_fun, deadline(1_000), 5)
      assert :counters.get(calls, 1) == 3
    end

    test "gives up once the propagation window closes, so the caller can roll back" do
      calls = :counters.new(1, [])

      start_fun = fn ->
        :counters.add(calls, 1, 1)
        :blocked
      end

      assert {:error, :name_release_timeout} =
               ConduitManager.start_until_free(start_fun, deadline(30), 5)

      assert :counters.get(calls, 1) > 1
    end
  end

  defp deadline(ms), do: System.monotonic_time(:millisecond) + ms
end
