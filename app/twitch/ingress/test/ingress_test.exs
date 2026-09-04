# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.PipelineTest do
  use ExUnit.Case, async: true

  alias Ingress.{JSON, LaneMessage, Pipeline}

  @special MapSet.new(["1001", "1002"])

  defp wire(message), do: message |> JSON.encode() |> IO.iodata_to_binary() |> JSON.decode()

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
                {"twitch.ingress.event.stream", %LaneMessage{lane: :stream} = live},
                {"twitch.ingress.event.premium", %LaneMessage{lane: :premium} = own}
              ]} = Pipeline.route(notification("stream.online", event), @meta)

      assert {:ok, %{"lane" => "stream", "type" => "stream.online", "event" => ^event}} =
               wire(live)

      assert {:ok, %{"lane" => "premium", "type" => "stream.online", "event" => ^event}} =
               wire(own)

      # Both copies of a live event share one encode of the non-lane members.
      assert live.body === own.body
    end

    test "stream.offline rides both the live lane and the broadcaster's event lane" do
      event = %{"broadcaster_user_id" => "77"}

      assert {:publish_many,
              [
                {"twitch.ingress.event.stream", %LaneMessage{lane: :stream} = live},
                {"twitch.ingress.event.premium", %LaneMessage{lane: :premium} = own}
              ]} = Pipeline.route(notification("stream.offline", event), @meta)

      assert {:ok, %{"lane" => "stream", "type" => "stream.offline"}} = wire(live)
      assert {:ok, %{"lane" => "premium", "type" => "stream.offline"}} = wire(own)
      assert live.body === own.body
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

    test "a non-chat event encodes the decoded event map onto the lane" do
      event = %{"broadcaster_user_id" => "77", "user_name" => "someone"}

      assert {:publish, "twitch.ingress.event.premium", message} =
               Pipeline.route(notification("channel.follow", event), @meta)

      assert {:ok, %{"event" => ^event}} = wire(message)
    end
  end

  describe "emote spans" do
    @meta %{shard_id: 0, msg_id: "m1", ts: "2026-06-10T00:00:00Z"}

    setup do
      start_supervised!({Task.Supervisor, name: Ingress.BroadcasterCache.TaskSupervisor})
      start_supervised!({Ingress.BroadcasterCache, [loader: fn _id -> {:ok, :premium} end]})
      :ok
    end

    defp chat_event(text, fragments) do
      %{
        "broadcaster_user_id" => "77",
        "chatter_user_id" => "555",
        "message" => %{"text" => text, "fragments" => fragments}
      }
    end

    test "mixed text, emote and cheermote fragments produce ordered id'd spans on the wire" do
      event =
        chat_event("hello Kappa world Cheer1000", [
          %{"type" => "text", "text" => "hello ", "emote" => nil},
          %{"type" => "emote", "text" => "Kappa", "emote" => %{"id" => "25"}},
          %{"type" => "text", "text" => " world ", "emote" => nil},
          %{
            "type" => "cheermote",
            "text" => "Cheer1000",
            "cheermote" => %{"prefix" => "Cheer", "bits" => 1000, "tier" => "1000"}
          }
        ])

      assert {:publish, _subject, message} =
               Pipeline.route(notification("channel.chat.message", event), @meta)

      assert message.emotes == [
               %{id: "25", begin: 6, end: 11},
               %{id: "Cheer", begin: 18, end: 27}
             ]

      assert {:ok, encoded} = wire(message)

      assert encoded["emotes"] == [
               %{"id" => "25", "begin" => 6, "end" => 11},
               %{"id" => "Cheer", "begin" => 18, "end" => 27}
             ]
    end

    test "offsets count codepoints: unicode before an emote pins the span past bytes and graphemes" do
      # Built from codepoints so editor normalization cannot silently change
      # the arithmetic: ñ (1 cp, 2 bytes), 🇺🇸 (2 cps, 8 bytes, 1 grapheme) and
      # a space give 4 codepoints — byte arithmetic says 12 and grapheme
      # arithmetic says 3, so only codepoint counting lands the span at 4.
      prefix = <<0x00F1::utf8>> <> <<0x1F1FA::utf8, 0x1F1F8::utf8>> <> " "

      assert String.length(prefix) == 3 and byte_size(prefix) == 11

      event =
        chat_event(prefix <> "Kappa", [
          %{"type" => "text", "text" => prefix, "emote" => nil},
          %{"type" => "emote", "text" => "Kappa", "emote" => %{"id" => "25"}}
        ])

      assert {:publish, _subject, %{emotes: [%{begin: 4, end: 9} = span]}} =
               Pipeline.route(notification("channel.chat.message", event), @meta)

      assert span.id == "25"
    end

    test "a ZWJ emoji counts every of its codepoints against later spans" do
      # 👨 + ZWJ + 👩 + ZWJ + 👧 is one grapheme built from five codepoints;
      # with the separating space the emote after it begins at 6, not at the
      # grapheme offset 2.
      zwj = "\u200D"
      prefix = "👨" <> zwj <> "👩" <> zwj <> "👧" <> " "

      assert String.length(prefix) == 2 and length(String.codepoints(prefix)) == 6

      event =
        chat_event(prefix <> "LUL", [
          %{"type" => "text", "text" => prefix, "emote" => nil},
          %{"type" => "emote", "text" => "LUL", "emote" => %{"id" => "425618"}}
        ])

      assert Pipeline.emote_spans(event) == [%{id: "425618", begin: 6, end: 9}]
    end

    test "chat without fragments or without emote fragments omits the emotes key entirely" do
      plain = %{
        "broadcaster_user_id" => "77",
        "chatter_user_id" => "555",
        "message" => %{"text" => "just chatting"}
      }

      assert {:publish, _subject, message} =
               Pipeline.route(notification("channel.chat.message", plain), @meta)

      refute Map.has_key?(message, :emotes)
      assert {:ok, encoded} = wire(message)
      refute Map.has_key?(encoded, "emotes")

      text_only =
        chat_event("gg", [
          %{"type" => "text", "text" => "gg", "emote" => nil}
        ])

      assert Pipeline.emote_spans(text_only) == []

      assert Pipeline.emote_spans(%{}) == []
      assert Pipeline.emote_spans(%{"message" => %{}}) == []
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

  test "unique production chat retains only its compact squash generation" do
    start_squash(window_ms: 10_000, sweep_ms: 10_000)

    event = %{
      "broadcaster_user_id" => "77",
      "broadcaster_user_login" => "channel",
      "chatter_user_id" => "1",
      "chatter_user_login" => "sender-only-marker",
      "badges" => [%{"set_id" => "marker"}]
    }

    meta = %{msg_id: "sender-message", ts: 0}

    assert Squash.observe_chat(:standard, event, "unique", meta) == :first

    assert [{{"77", "unique"}, expires_at, generation}] =
             :ets.tab2list(Ingress.Squash.Keys)

    assert is_integer(expires_at)
    assert is_reference(generation)
    assert :sys.get_state(Squash).cohorts == %{}
  end

  test "production chat allocates and emits sender details only for duplicates" do
    start_squash(window_ms: 20, sweep_ms: 10)

    event = fn id ->
      %{
        "broadcaster_user_id" => "77",
        "broadcaster_user_login" => "channel",
        "chatter_user_id" => id,
        "chatter_user_login" => "user#{id}",
        "badges" => []
      }
    end

    assert Squash.observe_chat(:standard, event.("1"), "same", %{msg_id: "m1", ts: 0}) ==
             :first

    assert Squash.observe_chat(:standard, event.("2"), "same", %{msg_id: "m2", ts: 0}) ==
             :buffered

    assert_receive {:published, _subject, cohort}, 500
    assert Enum.map(cohort.senders, & &1.chatter_user_login) == ["user2"]
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
    # The earliest buffered duplicate anchors the cohort's broker-side dedup id.
    assert cohort.msg_id == "m2"
  end

  test "a cohort at the size cap flushes early without waiting for the window" do
    start_squash(window_ms: 60_000, sweep_ms: 60_000, max_senders: 2)
    assert Squash.observe(base("flood"), sender("1")) == :first
    assert Squash.observe(base("flood"), sender("2")) == :buffered
    assert Squash.observe(base("flood"), sender("3")) == :buffered

    assert_receive {:published, _subject, cohort}, 500
    assert cohort.count == 2
  end

  test "opening a new window flushes an expired cohort instead of orphaning it" do
    start_squash(window_ms: 5, sweep_ms: 60_000)

    assert Squash.observe(base("race"), sender("1")) == :first
    assert Squash.observe(base("race"), sender("2")) == :buffered
    Process.sleep(10)

    # A caller can reach the expired row before the periodic sweep. Rotation is
    # serialized through the cohort owner so the old senders are emitted before
    # the new generation opens.
    assert Squash.observe(base("race"), sender("3")) == :first

    assert_receive {:published, _subject, cohort}, 500
    assert cohort.count == 1
    assert Enum.map(cohort.senders, & &1.chatter_user_id) == ["2"]
  end

  test "observe fails open to :first when the table is absent" do
    # No Squash started: the pipeline must never lose a message.
    assert Squash.observe(base("x"), sender("1")) == :first
  end

  test "folded cohorts carry the emote spans of the shared text" do
    start_squash(window_ms: 20, sweep_ms: 10)

    event = fn id ->
      %{
        "broadcaster_user_id" => "77",
        "broadcaster_user_login" => "channel",
        "chatter_user_id" => id,
        "chatter_user_login" => "user#{id}",
        "badges" => [],
        "message" => %{
          "text" => "gg Kappa",
          "fragments" => [
            %{"type" => "text", "text" => "gg ", "emote" => nil},
            %{"type" => "emote", "text" => "Kappa", "emote" => %{"id" => "25"}}
          ]
        }
      }
    end

    assert Squash.observe_chat(:standard, event.("1"), "gg Kappa", %{msg_id: "m1", ts: 0}) ==
             :first

    assert Squash.observe_chat(:standard, event.("2"), "gg Kappa", %{msg_id: "m2", ts: 0}) ==
             :buffered

    assert_receive {:published, _subject, cohort}, 500
    assert cohort.emotes == [%{id: "25", begin: 3, end: 8}]
  end

  test "cohorts of plain text carry no emotes key" do
    start_squash(window_ms: 20, sweep_ms: 10)

    event = fn id ->
      %{
        "broadcaster_user_id" => "77",
        "broadcaster_user_login" => "channel",
        "chatter_user_id" => id,
        "badges" => []
      }
    end

    assert Squash.observe_chat(:standard, event.("1"), "same", %{msg_id: "m1", ts: 0}) == :first

    assert Squash.observe_chat(:standard, event.("2"), "same", %{msg_id: "m2", ts: 0}) ==
             :buffered

    assert_receive {:published, _subject, cohort}, 500
    refute Map.has_key?(cohort, :emotes)
  end
end
