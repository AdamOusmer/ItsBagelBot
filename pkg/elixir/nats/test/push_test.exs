# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Platform.Nats.PushTest do
  use ExUnit.Case, async: false

  alias Platform.Nats.{Planes, Push}
  alias Platform.Nats.TestSupport.NatsServer

  @moduletag :tmp_dir
  @stream "PUSH_TEST"

  setup %{tmp_dir: dir} do
    NatsServer.start_planes(dir)
    config = %{name: @stream, subjects: ["feed.>"], storage: "memory"}
    {:ok, %{body: body}} = jetstream("$JS.API.STREAM.CREATE.#{@stream}", config)
    assert %{"config" => _} = Jason.decode!(body)
    %{jetstream: Planes.connection(:jetstream)}
  end

  test "stream range reports the first and last sequence", %{jetstream: js} do
    assert {:ok, 0, 0} = Push.stream_range(js, @stream)
    publish("feed.7.event", ~s({"n":1}))
    publish("feed.7.stats", ~s({"n":2}))
    assert {:ok, 1, 2} = Push.stream_range(js, @stream)
    assert :error = Push.stream_range(js, "MISSING")
  end

  test "a consumer starting at a sequence delivers in order with stream sequences", %{
    jetstream: js
  } do
    for n <- 1..3, do: publish("feed.7.event", ~s({"n":#{n}}))
    publish("feed.8.event", ~s({"n":9}))

    push = attached(js, "feed.7.>", {:sequence, 2})
    assert {2, ~s({"n":2}), push} = next_event(push)
    assert {3, ~s({"n":3}), push} = next_event(push)
    assert push.seq == 2

    publish("feed.7.stats", ~s({"n":4}))
    assert {5, ~s({"n":4}), %Push{seq: 3}} = next_event(push)
  end

  test "a new consumer skips history", %{jetstream: js} do
    publish("feed.7.event", ~s({"n":1}))
    push = attached(js, "feed.7.>", :new)
    publish("feed.7.event", ~s({"n":2}))
    assert {2, ~s({"n":2}), _push} = next_event(push)
  end

  test "an out of order delivery is refused", %{jetstream: js} do
    publish("feed.7.event", ~s({"n":1}))
    push = attached(js, "feed.7.>", {:sequence, 1})
    assert_receive {:msg, message}, 1_000
    assert {:error, :out_of_order} = Push.handle(%{push | seq: 4}, message)
  end

  test "a heartbeat that reports a missed delivery is a flow gap", %{jetstream: js} do
    push = %{attached(js, "feed.7.>", :new) | seq: 3}
    heartbeat = %{status: "100", reply_to: nil, headers: [{"nats-last-consumer", "5"}]}
    assert {:error, :flow_gap} = Push.handle(push, heartbeat)

    heartbeat = %{heartbeat | headers: [{"nats-last-consumer", "3"}]}
    assert {:ok, %Push{seq: 3}} = Push.handle(push, heartbeat)
  end

  test "an unattached push retries and an attached one goes stale", %{jetstream: js} do
    {:ok, push} = Push.open(js, @stream, "feed.7.>", :new, "test")
    assert {:ok, %Push{consumer: name}} = Push.tick(push)
    assert name == push.name

    stale = %{push | consumer: name, seen_at: System.monotonic_time(:millisecond) - 20_000}
    assert :stale = Push.tick(stale)
  end

  test "resume sequences come from stream ids" do
    assert Push.resume_sequence("41") == 42
    for bad <- ["0", "-1", "4x", "", "18446744073709551615", nil], do: assert(Push.resume_sequence(bad) == nil)
  end

  defp attached(jetstream, subject, start) do
    {:ok, push} = Push.open(jetstream, @stream, subject, start, "test")
    {:ok, push} = Push.attach(push)
    push
  end

  defp next_event(push) do
    assert_receive {:msg, message}, 2_000

    case Push.handle(push, message) do
      {:event, %{seq: seq, body: body}, push} -> {seq, body, push}
      {:ok, push} -> next_event(push)
    end
  end

  defp publish(subject, body) do
    {:ok, %{body: ack}} = Planes.jetstream_request({subject, body, [receive_timeout: 2_000]})
    assert %{"seq" => _} = Jason.decode!(ack)
  end

  defp jetstream(subject, body),
    do: Planes.jetstream_request({subject, Jason.encode!(body), [receive_timeout: 2_000]})
end
