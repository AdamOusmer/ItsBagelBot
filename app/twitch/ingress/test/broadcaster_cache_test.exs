# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.BroadcasterCacheTest do
  use ExUnit.Case, async: false

  alias Ingress.BroadcasterCache

  defp start_cache(loader, opts \\ []) do
    name = :"cache_#{System.unique_integer([:positive])}"

    start_supervised!({Task.Supervisor, name: Ingress.BroadcasterCache.TaskSupervisor})
    start_supervised!({BroadcasterCache, [name: name, table: name, loader: loader] ++ opts})

    name
  end

  defp counting_loader(result) do
    {:ok, counter} = Agent.start_link(fn -> 0 end)

    loader = fn _id ->
      Agent.update(counter, &(&1 + 1))
      result
    end

    {loader, counter}
  end

  test "read-through caches the loaded lane" do
    {loader, counter} = counting_loader({:ok, :premium})
    cache = start_cache(loader)

    assert BroadcasterCache.lane("b1", cache) == :premium
    assert BroadcasterCache.lane("b1", cache) == :premium
    assert Agent.get(counter, & &1) == 1
  end

  defp gated_loader(first) do
    test_pid = self()
    {:ok, calls} = Agent.start_link(fn -> 0 end)

    loader = fn _id ->
      case Agent.get_and_update(calls, &{&1, &1 + 1}) do
        0 -> first
        _ -> gate(test_pid)
      end
    end

    {loader, calls}
  end

  defp gate(test_pid) do
    send(test_pid, {:loading, self()})

    receive do
      {:release, result} -> result
    end
  end

  test "the sweep keeps recently expired entries and purges idle ones" do
    {loader, _counter} = counting_loader({:ok, :premium})
    cache = start_cache(loader, ttl_ms: 100, sweep_interval_ms: 10)

    assert BroadcasterCache.lane("b_old", cache) == :premium
    Process.sleep(150)
    assert :ets.info(cache, :size) == 1
    Process.sleep(100)
    assert :ets.info(cache, :size) == 0
  end

  for {name, refresh, after_refresh} <- [
        {"a refreshed lane", {:ok, :drop}, :drop},
        {"the last known lane after a failed refresh", {:error, :rpc_timeout}, :premium}
      ] do
    test "an expired entry is served while one background refresh yields #{name}" do
      {loader, calls} = gated_loader({:ok, :premium})
      cache = start_cache(loader, ttl_ms: 50)

      assert BroadcasterCache.lane("b2", cache) == :premium
      Process.sleep(70)
      lanes = Enum.map(1..50, fn _ -> BroadcasterCache.lane("b2", cache) end)
      assert Enum.uniq(lanes) == [:premium]
      assert_receive {:loading, refresh_pid}
      refute_receive {:loading, _}, 20

      send(refresh_pid, {:release, unquote(Macro.escape(refresh))})
      Process.sleep(20)
      Enum.each(1..10, fn _ -> BroadcasterCache.lane("b2", cache) end)
      refute_receive {:loading, _}, 20
      assert BroadcasterCache.lane("b2", cache) == unquote(after_refresh)
      assert Agent.get(calls, & &1) == 2
    end
  end

  test "invalidation of an expired entry loads the new lane before serving" do
    {loader, _calls} = gated_loader({:ok, :premium})
    cache = start_cache(loader, ttl_ms: 50)

    assert BroadcasterCache.lane("b7", cache) == :premium
    Process.sleep(70)
    BroadcasterCache.invalidate("b7", cache)
    lookup = Task.async(fn -> BroadcasterCache.lane("b7", cache) end)
    assert_receive {:loading, load_pid}

    send(load_pid, {:release, {:ok, :drop}})
    assert Task.await(lookup) == :drop
  end

  test "invalidation evicts a single broadcaster" do
    {loader, counter} = counting_loader({:ok, :premium})
    cache = start_cache(loader)

    assert BroadcasterCache.lane("b3", cache) == :premium
    BroadcasterCache.invalidate("b3", cache)
    assert BroadcasterCache.lane("b3", cache) == :premium
    assert Agent.get(counter, & &1) == 2
  end

  test "invalidate_all flushes every entry" do
    {loader, counter} = counting_loader({:ok, :premium})
    cache = start_cache(loader)

    assert BroadcasterCache.lane("b4", cache) == :premium
    assert BroadcasterCache.lane("b5", cache) == :premium
    BroadcasterCache.invalidate_all(cache)
    assert BroadcasterCache.lane("b4", cache) == :premium
    assert Agent.get(counter, & &1) == 3
  end

  test "loader failure degrades to standard and is negative-cached" do
    {loader, counter} = counting_loader({:error, :rpc_timeout})
    cache = start_cache(loader)

    assert BroadcasterCache.lane("b6", cache) == :standard
    assert BroadcasterCache.lane("b6", cache) == :standard
    assert Agent.get(counter, & &1) == 1
  end
end
