# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TraceTest do
  use ExUnit.Case, async: false

  alias Ingress.{Config, Trace}

  @message_id "3EJWWhwTjVUTN1nwZo4B_R6wUkNdP4XdVzzcHvFvlT0="

  test "a chat message carries its trace and ingress timings to the Go consumer" do
    headers =
      Trace.notification(System.monotonic_time(), %{}, %{msg_id: @message_id}, fn ->
        Trace.span("route", fn -> :routed end)
        Trace.headers_for(Config.lane_subject(:standard), nil)
      end)

    assert [{"traceparent", parent}, {"bagel-ingress", timings}] = headers
    assert parent =~ ~r/\A00-[0-9a-f]{32}-[0-9a-f]{16}-01\z/

    assert ["v1", started, ended, wait, @message_id, "route," <> step] =
             String.split(timings, ";")

    assert String.to_integer(ended) >= String.to_integer(started)
    assert String.to_integer(wait) >= 0
    assert [_offset, _duration] = String.split(step, ",")
  end

  test "only the chat lanes carry trace headers" do
    Trace.notification(System.monotonic_time(), %{}, %{msg_id: "event-1"}, fn ->
      assert Trace.headers_for("twitch.ingress.event.stream", nil) == []
      assert [_ | _] = Trace.headers_for(Config.lane_subject(:premium), nil)
    end)
  end

  test "trace state ends with the notification" do
    assert Trace.notification(System.monotonic_time(), %{}, %{}, fn -> :handled end) == :handled
    assert Trace.trace_headers() == []
    assert Trace.span("route", fn -> :untraced end) == :untraced
  end

  test "an id outside the wire alphabet is left out instead of breaking the header" do
    [_, {"bagel-ingress", timings}] =
      Trace.notification(System.monotonic_time(), %{}, %{msg_id: "a;b|c"}, fn ->
        Trace.trace_headers()
      end)

    assert ["v1", _, _, _, "", ""] = String.split(timings, ";")
  end
end
