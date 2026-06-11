defmodule Ingress.PipelineTest do
  use ExUnit.Case, async: true

  alias Ingress.Pipeline

  @special MapSet.new(["1001", "1002"])

  describe "decide/3" do
    test "special user goes premium even when the message is not a command" do
      assert Pipeline.decide("hello there", "1001", @special) == :special
    end

    test "special user wins over the command rule" do
      assert Pipeline.decide("!so someone", "1002", @special) == :special
    end

    test "command from a regular user routes by broadcaster lane" do
      assert Pipeline.decide("!points", "555", @special) == :command
    end

    test "leading whitespace does not hide a command" do
      assert Pipeline.decide("   !points", "555", @special) == :command
    end

    test "plain chatter from a regular user is dropped" do
      assert Pipeline.decide("just chatting", "555", @special) == :drop
    end

    test "empty text from an unknown user is dropped" do
      assert Pipeline.decide("", nil, @special) == :drop
    end

    test "bang in the middle of the message is not a command" do
      assert Pipeline.decide("nice play!", "555", @special) == :drop
    end
  end

  describe "route/2 stream lane" do
    @meta %{shard_id: 0, msg_id: "m1", ts: "2026-06-10T00:00:00Z"}

    defp notification(type, event) do
      %{"subscription" => %{"type" => type}, "event" => event}
    end

    test "stream.online goes to the dedicated stream lane" do
      event = %{"broadcaster_user_id" => "77", "type" => "live"}

      assert {:publish, "twitch.ingress.event.stream", %{lane: :stream, type: "stream.online"}} =
               Pipeline.route(notification("stream.online", event), @meta)
    end

    test "stream.offline goes to the dedicated stream lane" do
      event = %{"broadcaster_user_id" => "77"}

      assert {:publish, "twitch.ingress.event.stream", %{lane: :stream, type: "stream.offline"}} =
               Pipeline.route(notification("stream.offline", event), @meta)
    end

    test "stream events never touch the broadcaster cache" do
      # No BroadcasterCache is running in this test; a status lookup would
      # crash the call. Routing must not need one.
      event = %{"broadcaster_user_id" => "77"}
      assert {:publish, _, _} = Pipeline.route(notification("stream.online", event), @meta)
    end

    test "special-user chat routes premium without the cache" do
      event = %{
        "broadcaster_user_id" => "77",
        "chatter_user_id" => "1001",
        "message" => %{"text" => "hello"}
      }

      Application.put_env(:ingress, :special_user_ids, @special)
      on_exit(fn -> Application.put_env(:ingress, :special_user_ids, MapSet.new()) end)

      assert {:publish, "twitch.ingress.event.premium", %{lane: :premium}} =
               Pipeline.route(notification("channel.chat.message", event), @meta)
    end
  end

  describe "broadcaster_id/1" do
    test "channel events carry broadcaster_user_id" do
      assert Pipeline.broadcaster_id(%{"broadcaster_user_id" => "77"}) == "77"
    end

    test "inbound raids identify the receiving channel" do
      event = %{"from_broadcaster_user_id" => "11", "to_broadcaster_user_id" => "77"}
      assert Pipeline.broadcaster_id(event) == "77"
    end

    test "events without a broadcaster yield nil" do
      assert Pipeline.broadcaster_id(%{"user_id" => "5"}) == nil
    end
  end
end

defmodule Ingress.BroadcasterCacheTest do
  use ExUnit.Case, async: false

  alias Ingress.BroadcasterCache

  defp start_cache(loader, opts \\ []) do
    name = :"cache_#{System.unique_integer([:positive])}"

    start_supervised!(
      {BroadcasterCache, [name: name, table: name, loader: loader] ++ opts}
    )

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

  test "the sweep purges expired entries that are never read again" do
    {loader, _counter} = counting_loader({:ok, :premium})
    cache = start_cache(loader, ttl_ms: 10, sweep_interval_ms: 20)

    assert BroadcasterCache.lane("b_old", cache) == :premium
    assert :ets.info(cache, :size) == 1
    Process.sleep(60)
    assert :ets.info(cache, :size) == 0
  end

  test "entries expire after the TTL" do
    {loader, counter} = counting_loader({:ok, :standard})
    cache = start_cache(loader, ttl_ms: 30)

    assert BroadcasterCache.lane("b2", cache) == :standard
    Process.sleep(50)
    assert BroadcasterCache.lane("b2", cache) == :standard
    assert Agent.get(counter, & &1) == 2
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
    # negative-cached: immediate retry does not hit the loader again
    assert BroadcasterCache.lane("b6", cache) == :standard
    assert Agent.get(counter, & &1) == 1
  end
end

defmodule Ingress.CacheInvalidatorTest do
  @moduledoc """
  Drives the NATS consumer callback directly against the cache instance the
  application runs (default name and table), proving an invalidation message
  on the bus actually evicts in-process entries.
  """

  use ExUnit.Case, async: false

  alias Ingress.{BroadcasterCache, CacheInvalidator}

  setup do
    {:ok, counter} = Agent.start_link(fn -> 0 end)

    loader = fn _id ->
      Agent.update(counter, &(&1 + 1))
      {:ok, :premium}
    end

    start_supervised!({BroadcasterCache, [loader: loader, ttl_ms: 60_000]})
    %{counter: counter}
  end

  defp loads(counter), do: Agent.get(counter, & &1)

  test ~s({"broadcaster_id": ...} evicts that broadcaster), %{counter: counter} do
    assert BroadcasterCache.lane("b1") == :premium
    assert :ok = CacheInvalidator.request(%{body: ~s({"broadcaster_id": "b1"})})
    assert BroadcasterCache.lane("b1") == :premium
    assert loads(counter) == 2
  end

  test ~s({"all": true} flushes the cache), %{counter: counter} do
    assert BroadcasterCache.lane("b1") == :premium
    assert BroadcasterCache.lane("b2") == :premium
    assert :ok = CacheInvalidator.request(%{body: ~s({"all": true})})
    assert BroadcasterCache.lane("b1") == :premium
    assert loads(counter) == 3
  end

  test "a bare broadcaster ID as body evicts", %{counter: counter} do
    assert BroadcasterCache.lane("b1") == :premium
    assert :ok = CacheInvalidator.request(%{body: "b1"})
    assert BroadcasterCache.lane("b1") == :premium
    assert loads(counter) == 2
  end

  test "an unrelated invalidation leaves entries cached", %{counter: counter} do
    assert BroadcasterCache.lane("b1") == :premium
    assert :ok = CacheInvalidator.request(%{body: ~s({"broadcaster_id": "other"})})
    assert BroadcasterCache.lane("b1") == :premium
    assert loads(counter) == 1
  end

  test "garbage bodies are ignored without crashing the consumer" do
    assert :ok = CacheInvalidator.request(%{body: ""})
    assert :ok = CacheInvalidator.request(%{body: "   "})
  end
end
