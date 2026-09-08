# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.TraceTest do
  use ExUnit.Case, async: false

  import Ingress.EnvCase

  alias Ingress.Trace

  test "disabled tracing bypasses transaction work and still runs the event" do
    put_env(trace_sample_rate: 0)

    assert Trace.notification(System.monotonic_time(), %{}, %{}, fn -> :handled end) == :handled
    assert Trace.trace_headers() == []
  end

  test "diagnostic sampling cleans up transaction-local trace state" do
    put_env(trace_sample_rate: 1)

    assert Trace.notification(System.monotonic_time(), %{}, %{msg_id: "event-1"}, fn ->
             assert is_list(Trace.trace_headers())
             :handled
           end) == :handled

    assert Trace.trace_headers() == []
  end

  test "destination and result facets stay finite" do
    assert Trace.destination("twitch.ingress.event.standard") ==
             "twitch.ingress.event.standard"

    assert Trace.destination("twitch.ingress.event.tenant.123") ==
             "twitch.ingress.event.other"

    assert Trace.result({:error, :timeout}) == "timeout"
    assert Trace.result({:error, :anything}) == "error"
    assert Trace.result(:squash) == "filtered"
  end
end
