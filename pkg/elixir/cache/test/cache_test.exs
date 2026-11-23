# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.CacheTest do
  use ExUnit.Case, async: true

  import ExUnit.CaptureLog

  alias Platform.Cache

  defmodule FakeFeed do
    def start_link, do: Agent.start_link(fn -> %{watchers: [], gaps: []} end)

    def watch(feed, scope, fun, owner),
      do: Agent.update(feed, &update_in(&1.watchers, fn list -> [{scope, fun, owner} | list] end))

    def watch_gap(feed, fun, _owner),
      do: Agent.update(feed, &update_in(&1.gaps, fn list -> [fun | list] end))

    def emit(feed, scope, event) do
      for {^scope, fun, _owner} <- Agent.get(feed, & &1.watchers), do: fun.(event)
      :ok
    end

    def callbacks(feed),
      do: Agent.get(feed, &{Enum.map(&1.watchers, fn {_, fun, _} -> fun end), &1.gaps})

    def gap(feed), do: Enum.each(Agent.get(feed, & &1.gaps), & &1.())
  end

  defp start_cache(opts \\ []) do
    name = :"cache_#{System.unique_integer([:positive])}"
    defaults = [name: name, ttl_ms: 60_000, capacity: 100]
    start_supervised!({Cache, Keyword.merge(defaults, opts)})
    name
  end

  defp invalidate_everything(cache, :clear), do: Cache.clear(cache)

  defp invalidate_everything(cache, :invalidate_where),
    do: Cache.invalidate_where(cache, fn _key -> true end)

  defp counting(result) do
    {:ok, counter} = Agent.start_link(fn -> 0 end)

    loader = fn ->
      Agent.update(counter, &(&1 + 1))
      result
    end

    {loader, fn -> Agent.get(counter, & &1) end}
  end

  defp gated(value) do
    test_pid = self()

    fn ->
      send(test_pid, {:loading, self()})

      receive do
        :release -> {:ok, value}
      end
    end
  end

  test "a loaded value is served from the table until it expires" do
    cache = start_cache(ttl_ms: 100)
    {loader, calls} = counting({:ok, :v})

    assert {:ok, :v} = Cache.get_or_load(cache, :k, loader)
    assert {:ok, :v} = Cache.get_or_load(cache, :k, loader)
    assert calls.() == 1

    Process.sleep(250)
    assert {:ok, :v} = Cache.get_or_load(cache, :k, loader)
    assert calls.() == 2
  end

  for {name, result, reply} <- [
        {"skip", {:skip, :v}, {:ok, :v}},
        {"error", {:error, :down}, {:error, :down}}
      ] do
    test "a #{name} result is returned and not stored" do
      cache = start_cache()
      {loader, calls} = counting(unquote(Macro.escape(result)))

      assert Cache.get_or_load(cache, :k, loader) == unquote(Macro.escape(reply))
      assert Cache.get_or_load(cache, :k, loader) == unquote(Macro.escape(reply))
      assert calls.() == 2
      assert Cache.size(cache) == 0
    end
  end

  test "refresh_ahead serves the expired value while one background reload runs" do
    cache = start_cache(ttl_ms: 50, stale_ms: 60_000, refresh_ahead: true)
    assert {:ok, :old} = Cache.get_or_load(cache, :k, fn -> {:ok, :old} end)
    Process.sleep(120)

    reload = gated(:new)
    results = Enum.map(1..20, fn _ -> Cache.get_or_load(cache, :k, reload) end)
    assert Enum.uniq(results) == [{:ok, :old}]
    assert_receive {:loading, pid}
    refute_receive {:loading, _}, 30

    send(pid, :release)
    Process.sleep(50)
    assert {:ok, :new} = Cache.get_or_load(cache, :k, reload)
  end

  test "refresh_ahead queues one refresh per stale period" do
    cache = start_cache(ttl_ms: 50, stale_ms: 60_000, refresh_ahead: true)
    assert {:ok, :old} = Cache.get_or_load(cache, :k, fn -> {:ok, :old} end)
    Process.sleep(120)

    pid = Process.whereis(cache)
    :sys.suspend(pid)
    results = Enum.map(1..50, fn _ -> Cache.get_or_load(cache, :k, fn -> {:ok, :new} end) end)
    assert {:message_queue_len, 1} = Process.info(pid, :message_queue_len)
    :sys.resume(pid)
    assert Enum.uniq(results) == [{:ok, :old}]
  end

  test "a failed background refresh releases the claim so a later read retries" do
    cache = start_cache(ttl_ms: 50, stale_ms: 60_000, refresh_ahead: true)
    assert {:ok, :v} = Cache.get_or_load(cache, :k, fn -> {:ok, :v} end)
    Process.sleep(120)
    {failing, calls} = counting({:error, :down})

    Enum.each(1..2, fn _ ->
      assert {:ok, :v} = Cache.get_or_load(cache, :k, failing)
      Process.sleep(40)
    end)

    assert calls.() == 2
  end

  test "refresh_ahead emits stale and blocks on a miss" do
    cache = start_cache(ttl_ms: 50, stale_ms: 60_000, refresh_ahead: true)
    test = self()

    :telemetry.attach_many(
      "refresh-events-#{cache}",
      Enum.map([:miss, :stale], &[:platform, :cache, &1]),
      fn [:platform, :cache, event], _measurements, %{name: name}, _config ->
        send(test, {:cache_event, name, event})
      end,
      nil
    )

    on_exit(fn -> :telemetry.detach("refresh-events-#{cache}") end)

    assert {:ok, :v} = Cache.get_or_load(cache, :k, fn -> {:ok, :v} end)
    Process.sleep(120)
    assert {:ok, :v} = Cache.get_or_load(cache, :k, fn -> {:ok, :v} end)

    for event <- [:miss, :stale] do
      assert_receive {:cache_event, ^cache, ^event}
    end
  end

  for refresh_ahead <- [false, true] do
    test "error_ttl_ms keeps the previous value instead of reloading per call (refresh_ahead #{refresh_ahead})" do
      cache =
        start_cache(
          ttl_ms: 50,
          stale_ms: 60_000,
          error_ttl_ms: 60_000,
          refresh_ahead: unquote(refresh_ahead)
        )

      assert {:ok, :v} = Cache.get_or_load(cache, :k, fn -> {:ok, :v} end)
      Process.sleep(120)
      {failing, calls} = counting({:error, :down})

      Enum.each(1..5, fn _ ->
        assert {:ok, :v} = Cache.get_or_load(cache, :k, failing)
        Process.sleep(10)
      end)

      assert calls.() == 1
    end
  end

  test "error_default is stored for error_ttl_ms when no previous value exists" do
    cache = start_cache(error_ttl_ms: 60_000, error_default: :fallback)
    {failing, calls} = counting({:error, :down})

    Enum.each(1..3, fn _ -> assert {:ok, :fallback} = Cache.get_or_load(cache, :k, failing) end)
    assert calls.() == 1
  end

  test "hits, misses, stale serves, invalidations and gaps emit telemetry" do
    cache = start_cache(ttl_ms: 100, stale_ms: 60_000)
    {:ok, feed} = FakeFeed.start_link()
    Cache.invalidate_on(cache, {FakeFeed, feed}, & &1.keys, ["s"])
    test = self()

    :telemetry.attach_many(
      "cache-events-#{cache}",
      Enum.map([:hit, :miss, :stale, :invalidated, :gap], &[:platform, :cache, &1]),
      fn [:platform, :cache, event], _measurements, %{name: name}, _config ->
        send(test, {:cache_event, name, event})
      end,
      nil
    )

    on_exit(fn -> :telemetry.detach("cache-events-#{cache}") end)

    assert {:ok, :v} = Cache.get_or_load(cache, :k, fn -> {:ok, :v} end)
    assert {:ok, :v} = Cache.get_or_load(cache, :k, fn -> {:ok, :v} end)
    Process.sleep(250)
    assert {:ok, :v} = Cache.get_or_load(cache, :k, fn -> {:error, :down} end)
    FakeFeed.emit(feed, "s", %{keys: [:k]})
    FakeFeed.gap(feed)

    for event <- [:miss, :hit, :miss, :stale, :invalidated, :gap] do
      assert_receive {:cache_event, ^cache, ^event}
    end
  end

  test "a crashing loader is an error and is not stored" do
    cache = start_cache()
    loaders = [fn -> exit(:boom) end, fn -> raise "boom" end]

    test_pid = self()

    capture_log(fn ->
      send(test_pid, {:results, Enum.map(loaders, &Cache.get_or_load(cache, :k, &1))})
    end)

    assert_received {:results, [exit_result, raise_result]}
    assert exit_result == {:error, {:loader_exit, :boom}}
    assert {:error, {:loader_exit, {%RuntimeError{}, _stacktrace}}} = raise_result
    assert Cache.size(cache) == 0
  end

  test "a loader past the load timeout is killed" do
    cache = start_cache(load_timeout_ms: 50)

    assert {:error, {:loader_exit, :killed}} =
             Cache.get_or_load(cache, :k, fn -> Process.sleep(:infinity) end)
  end

  test "an unexpected loader return crashes the cache" do
    cache = start_cache()

    capture_log(fn ->
      assert {_reason, _call} = catch_exit(Cache.get_or_load(cache, :k, fn -> :bad end))
    end)
  end

  test "concurrent callers share one load" do
    cache = start_cache()
    loader = gated(:shared)

    callers = for _ <- 1..20, do: Task.async(fn -> Cache.get_or_load(cache, :k, loader) end)
    assert_receive {:loading, loader_pid}
    Process.sleep(30)
    send(loader_pid, :release)

    assert Enum.uniq(Task.await_many(callers)) == [{:ok, :shared}]
    refute_receive {:loading, _}, 50
  end

  test "a load that races an invalidation is returned but not stored" do
    cache = start_cache()
    loader = gated(:old)
    caller = Task.async(fn -> Cache.get_or_load(cache, :k, loader) end)
    assert_receive {:loading, loader_pid}

    Cache.invalidate(cache, :k)
    send(loader_pid, :release)

    assert {:ok, :old} = Task.await(caller)
    assert Cache.size(cache) == 0
  end

  test "a caller after an invalidation starts its own load" do
    cache = start_cache()
    old_loader = gated(:old)
    new_loader = gated(:new)
    first = Task.async(fn -> Cache.get_or_load(cache, :k, old_loader) end)
    assert_receive {:loading, old_pid}

    Cache.invalidate(cache, :k)
    second = Task.async(fn -> Cache.get_or_load(cache, :k, new_loader) end)
    assert_receive {:loading, new_pid}
    send(new_pid, :release)
    send(old_pid, :release)

    assert {:ok, :new} = Task.await(second)
    assert {:ok, :old} = Task.await(first)
    assert {:ok, :new} = Cache.get_or_load(cache, :k, fn -> {:error, :unused} end)
  end

  for {stale_ms, expected} <- [{0, {:error, :down}}, {1_000, {:ok, :v}}] do
    test "an error after expiry with a #{stale_ms} ms stale window returns #{inspect(expected)}" do
      cache = start_cache(ttl_ms: 100, stale_ms: unquote(stale_ms))
      assert {:ok, :v} = Cache.get_or_load(cache, :k, fn -> {:ok, :v} end)

      Process.sleep(250)

      assert Cache.get_or_load(cache, :k, fn -> {:error, :down} end) ==
               unquote(Macro.escape(expected))
    end
  end

  test "an invalidated entry is not served stale" do
    cache = start_cache(ttl_ms: 100, stale_ms: 5_000)
    assert {:ok, :v} = Cache.get_or_load(cache, :k, fn -> {:ok, :v} end)

    Process.sleep(250)
    Cache.invalidate(cache, :k)
    assert {:error, :down} = Cache.get_or_load(cache, :k, fn -> {:error, :down} end)
  end

  test "a full cache evicts expired entries first, then the oldest" do
    cache = start_cache(capacity: 3, ttl_ms: 60_000)
    for key <- 1..3, do: Cache.get_or_load(cache, key, fn -> {:ok, key} end)

    assert {:ok, 4} = Cache.get_or_load(cache, 4, fn -> {:ok, 4} end)
    assert Cache.size(cache) == 3
    assert {:ok, :reloaded} = Cache.get_or_load(cache, 1, fn -> {:ok, :reloaded} end)
    assert {:ok, 4} = Cache.get_or_load(cache, 4, fn -> {:ok, :unused} end)
  end

  test "a full cache drops an expired entry before a live one" do
    cache = start_cache(capacity: 3, ttl_ms: 100)
    for key <- [1, 2], do: Cache.get_or_load(cache, key, fn -> {:ok, key} end)
    Process.sleep(250)
    for key <- [3, 4], do: Cache.get_or_load(cache, key, fn -> {:ok, key} end)

    assert Cache.size(cache) == 2
    assert {:ok, 3} = Cache.get_or_load(cache, 3, fn -> {:ok, :unused} end)
  end

  for action <- [:clear, :invalidate_where] do
    test "#{action} racing an in-flight load keeps the result out of the cache" do
      cache = start_cache()
      loader = gated(:old)
      caller = Task.async(fn -> Cache.get_or_load(cache, :k, loader) end)
      assert_receive {:loading, loader_pid}

      invalidate_everything(cache, unquote(action))
      send(loader_pid, :release)

      assert {:ok, :old} = Task.await(caller)
      assert Cache.get_or_load(cache, :k, fn -> {:ok, :fresh} end) == {:ok, :fresh}
    end
  end

  test "keys_fun may return :all or a where predicate" do
    {:ok, feed} = FakeFeed.start_link()
    cache = start_cache(invalidate_on: {{FakeFeed, feed}, & &1.drop, ["s"]})

    for {drop, left} <- [{:all, 0}, {{:where, &(&1 in ["a", "b"])}, 1}, {["a"], 2}] do
      for key <- ["a", "b", "c"], do: Cache.get_or_load(cache, key, fn -> {:ok, key} end)
      FakeFeed.emit(feed, "s", %{drop: drop})
      :sys.get_state(cache)
      assert Cache.size(cache) == left
    end
  end

  test "feed callbacks of a stopped cache are a no-op" do
    {:ok, feed} = FakeFeed.start_link()
    cache = start_cache(invalidate_on: {{FakeFeed, feed}, & &1.drop, ["s"]})
    {[watcher], [gap]} = FakeFeed.callbacks(feed)
    stop_supervised!({Cache, cache})

    assert watcher.(%{drop: :all}) == :ok
    assert gap.() == :ok
  end

  test "the invalidated event is emitted only when a row was removed" do
    cache = start_cache()
    handler = "invalidated-#{cache}"
    test_pid = self()

    :telemetry.attach(
      handler,
      [:platform, :cache, :invalidated],
      fn _event, _measurements, %{name: name}, _config ->
        send(test_pid, {:invalidated, name})
      end,
      nil
    )

    on_exit(fn -> :telemetry.detach(handler) end)
    Cache.invalidate(cache, :missing)
    refute_receive {:invalidated, ^cache}, 100

    Cache.get_or_load(cache, :k, fn -> {:ok, :v} end)
    Cache.invalidate(cache, :k)
    assert_receive {:invalidated, ^cache}
  end

  for {value, expected} <- [{:negative, {:error, :down}}, {:positive, {:ok, :positive}}] do
    test "stale_if decides whether a #{value} entry is served on error" do
      cache = start_cache(ttl_ms: 100, stale_ms: 5_000, stale_if: &(&1 != :negative))
      assert {:ok, _} = Cache.get_or_load(cache, :k, fn -> {:ok, unquote(value)} end)

      Process.sleep(250)

      assert Cache.get_or_load(cache, :k, fn -> {:error, :down} end) ==
               unquote(Macro.escape(expected))
    end
  end

  test "the invalidate_on child option registers the cache with the feed" do
    {:ok, feed} = FakeFeed.start_link()
    cache = start_cache(invalidate_on: {{FakeFeed, feed}, & &1.keys, ["overlays"]})
    assert [{"overlays", _fun, ^cache}] = Agent.get(feed, & &1.watchers)
    for key <- ["a", "b"], do: Cache.get_or_load(cache, key, fn -> {:ok, key} end)

    FakeFeed.emit(feed, "overlays", %{keys: ["a"]})
    assert Cache.size(cache) == 1

    FakeFeed.gap(feed)
    assert Cache.size(cache) == 0
  end

  test "invalidate_where drops matching keys and clear drops all" do
    cache = start_cache()
    for key <- [1, 2, 3, 4], do: Cache.get_or_load(cache, key, fn -> {:ok, key} end)

    :ok = Cache.invalidate_where(cache, &(rem(&1, 2) == 0))
    assert Cache.size(cache) == 2

    :ok = Cache.clear(cache)
    assert Cache.size(cache) == 0
  end

  test "invalidate_on drops the keys an event names and clears on a gap" do
    cache = start_cache()
    {:ok, feed} = FakeFeed.start_link()
    :ok = Cache.invalidate_on(cache, {FakeFeed, feed}, & &1.keys, ["overlays"])
    for key <- ["a", "b"], do: Cache.get_or_load(cache, key, fn -> {:ok, key} end)

    FakeFeed.emit(feed, "overlays", %{scope: "overlays", id: 7, keys: ["a"]})
    assert Cache.size(cache) == 1

    FakeFeed.emit(feed, "other", %{scope: "other", id: 7, keys: ["b"]})
    assert Cache.size(cache) == 1

    FakeFeed.gap(feed)
    assert Cache.size(cache) == 0
  end
end
