defmodule Ingress.TraceTest do
  use ExUnit.Case, async: false

  alias Ingress.Trace

  setup do
    previous = Application.get_env(:ingress, :trace_sample_rate)

    on_exit(fn ->
      if is_nil(previous) do
        Application.delete_env(:ingress, :trace_sample_rate)
      else
        Application.put_env(:ingress, :trace_sample_rate, previous)
      end
    end)

    :ok
  end

  test "disabled tracing bypasses transaction work and still runs the event" do
    Application.put_env(:ingress, :trace_sample_rate, 0)

    assert Trace.notification(System.monotonic_time(), %{}, %{}, fn -> :handled end) == :handled
    assert Trace.trace_headers() == []
  end

  test "diagnostic sampling cleans up transaction-local trace state" do
    Application.put_env(:ingress, :trace_sample_rate, 1)

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
