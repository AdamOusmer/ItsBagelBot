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

    test "plain chatter from a regular user is chat (published, then squashed)" do
      assert Pipeline.decide("just chatting", "555", @special) == :chat
    end

    test "empty text from an unknown user is chat" do
      assert Pipeline.decide("", nil, @special) == :chat
    end

    test "bang in the middle of the message is not a command" do
      assert Pipeline.decide("nice play!", "555", @special) == :chat
    end
  end

  describe "route/2 stream lane" do
    @meta %{shard_id: 0, msg_id: "m1", ts: "2026-06-10T00:00:00Z"}

    setup do
      # The live event is now dual-published, so routing reads the broadcaster
      # status for the event-lane copy. Stand up a cache that always returns
      # premium for these tests.
      start_supervised!({Task.Supervisor, name: Ingress.BroadcasterCache.TaskSupervisor})
      start_supervised!({Ingress.BroadcasterCache, [loader: fn _id -> {:ok, :premium} end]})
      :ok
    end

    defp notification(type, event) do
      %{"subscription" => %{"type" => type}, "event" => event}
    end

    test "stream.online rides both the live lane and the broadcaster's event lane" do
      event = %{"broadcaster_user_id" => "77", "type" => "live"}

      assert {:publish_many,
              [
                {"twitch.ingress.event.stream", %{lane: :stream, type: "stream.online"}},
                {"twitch.ingress.event.premium", %{lane: :premium, type: "stream.online"}}
              ]} = Pipeline.route(notification("stream.online", event), @meta)
    end

    test "stream.offline rides both the live lane and the broadcaster's event lane" do
      event = %{"broadcaster_user_id" => "77"}

      assert {:publish_many,
              [
                {"twitch.ingress.event.stream", %{lane: :stream, type: "stream.offline"}},
                {"twitch.ingress.event.premium", %{lane: :premium, type: "stream.offline"}}
              ]} = Pipeline.route(notification("stream.offline", event), @meta)
    end

    test "a live event without a broadcaster still hits the live lane on the standard event lane" do
      event = %{"type" => "live"}

      assert {:publish_many,
              [
                {"twitch.ingress.event.stream", %{lane: :stream}},
                {"twitch.ingress.event.standard", %{lane: :standard}}
              ]} = Pipeline.route(notification("stream.online", event), @meta)
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

    test "chat routing forwards the chatter badges for downstream permission checks" do
      badges = [%{"set_id" => "lead_moderator", "id" => "1"}]

      event = %{
        "broadcaster_user_id" => "77",
        "chatter_user_id" => "1001",
        "message" => %{"text" => "!ban someone"},
        "badges" => badges
      }

      Application.put_env(:ingress, :special_user_ids, @special)
      on_exit(fn -> Application.put_env(:ingress, :special_user_ids, MapSet.new()) end)

      assert {:publish, _subject, %{badges: ^badges}} =
               Pipeline.route(notification("channel.chat.message", event), @meta)
    end

    test "oversized chat text is dropped before routing" do
      event = %{
        "broadcaster_user_id" => "77",
        "chatter_user_id" => "555",
        "message" => %{"text" => String.duplicate("a", 5_000)}
      }

      assert :oversized = Pipeline.route(notification("channel.chat.message", event), @meta)
    end

    test "plain chat publishes to the broadcaster lane (squash fails open when unstarted)" do
      event = %{
        "broadcaster_user_id" => "77",
        "chatter_user_id" => "555",
        "message" => %{"text" => "just chatting"}
      }

      assert {:publish, "twitch.ingress.event.premium",
              %{type: "channel.chat.message", lane: :premium, text: "just chatting"}} =
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

    start_supervised!({Task.Supervisor, name: Ingress.BroadcasterCache.TaskSupervisor})
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

defmodule Ingress.SquashTest do
  use ExUnit.Case, async: false

  alias Ingress.Squash

  defp start_squash(opts) do
    test = self()
    publish = fn subject, msg -> send(test, {:published, subject, msg}) end
    start_supervised!({Squash, [publish: publish] ++ opts})
    :ok
  end

  defp base(text, lane \\ :standard),
    do: %{broadcaster_user_id: "77", broadcaster_user_login: "chan", lane: lane, text: text}

  defp sender(id),
    do: %{chatter_user_id: id, chatter_user_login: "u#{id}", msg_id: "m#{id}", ts: 0, badges: nil}

  test "the first identical line is :first, the rest are :buffered" do
    start_squash(window_ms: 10_000, sweep_ms: 10_000)
    assert Squash.observe(base("gg"), sender("1")) == :first
    assert Squash.observe(base("gg"), sender("2")) == :buffered
    assert Squash.observe(base("gg"), sender("3")) == :buffered
  end

  test "distinct text opens distinct windows (both :first)" do
    start_squash(window_ms: 10_000, sweep_ms: 10_000)
    assert Squash.observe(base("aaa"), sender("1")) == :first
    assert Squash.observe(base("bbb"), sender("1")) == :first
  end

  test "the window flushes one cohort carrying every duplicate sender in order" do
    start_squash(window_ms: 20, sweep_ms: 10)
    assert Squash.observe(base("spam", :premium), sender("1")) == :first
    assert Squash.observe(base("spam", :premium), sender("2")) == :buffered
    assert Squash.observe(base("spam", :premium), sender("3")) == :buffered

    assert_receive {:published, "twitch.ingress.event.premium", cohort}, 500
    assert cohort.type == "channel.chat.message"
    assert cohort.text == "spam"
    assert cohort.count == 2
    assert cohort.distinct_users == 2
    assert Enum.map(cohort.senders, & &1.chatter_user_id) == ["2", "3"]
  end

  test "a cohort at the size cap flushes early without waiting for the window" do
    start_squash(window_ms: 60_000, sweep_ms: 60_000, max_senders: 2)
    assert Squash.observe(base("flood"), sender("1")) == :first
    assert Squash.observe(base("flood"), sender("2")) == :buffered
    assert Squash.observe(base("flood"), sender("3")) == :buffered

    assert_receive {:published, _subject, cohort}, 500
    assert cohort.count == 2
  end

  test "observe fails open to :first when the table is absent" do
    # No Squash started: the pipeline must never lose a message.
    assert Squash.observe(base("x"), sender("1")) == :first
  end
end
