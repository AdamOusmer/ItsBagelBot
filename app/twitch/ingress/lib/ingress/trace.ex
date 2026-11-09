# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Trace do
  @moduledoc false

  alias Ingress.Config

  @active_key {__MODULE__, :active}
  @policy %{
    "dispatcher.wait_ms": :number,
    "event.type": {:enum, ["chat", "other", "invalid"]},
    result: {:enum, ["ok", "timeout", "error", "filtered", "invalid", "dropped"]},
    dependency: {:enum, ["nats"]},
    "event.lane": {:enum, ["premium", "standard", "stream", "other", "none"]},
    "messaging.destination":
      {:enum,
       [
         "twitch.ingress.event.premium",
         "twitch.ingress.event.standard",
         "twitch.ingress.event.stream",
         "twitch.ingress.event.other"
       ]}
  }

  def notification(enqueued_at, payload, meta, fun) when is_function(fun, 0) do
    if sampled?(meta) do
      traced_notification(enqueued_at, payload, fun)
    else
      fun.()
    end
  end

  defp traced_notification(enqueued_at, payload, fun) do
    wait_us =
      System.monotonic_time()
      |> Kernel.-(enqueued_at)
      |> max(0)
      |> System.convert_time_unit(:native, :microsecond)

    Process.put(@active_key, true)

    try do
      Platform.NewRelic.transaction(
        "Ingress",
        "Notification",
        ["dispatcher.wait_ms": wait_us / 1_000, "event.type": event_type(payload)],
        fun,
        @policy
      )
    after
      Process.delete(@active_key)
    end
  end

  defp sampled?(meta) do
    case Config.trace_sample_rate() do
      rate when rate <= 0 -> false
      1 -> true
      rate -> :erlang.phash2({Map.get(meta, :msg_id), Map.get(meta, :shard_id)}, rate) == 0
    end
  end

  defp event_type(%{"subscription" => %{"type" => "channel.chat.message"}}), do: "chat"
  defp event_type(%{"subscription" => %{"type" => type}}) when is_binary(type), do: "other"
  defp event_type(_payload), do: "invalid"

  def trace_headers do
    if active?() do
      Platform.NewRelic.trace_headers()
    else
      []
    end
  end

  def span(name, attributes \\ [], fun) when is_function(fun, 0) do
    if active?() do
      Platform.NewRelic.span(name, attributes, fun, @policy)
    else
      fun.()
    end
  end

  def add_span_attributes(attributes) do
    if active?(), do: Platform.NewRelic.add_span_attributes(attributes, @policy)
    :ok
  end

  defp active?, do: Process.get(@active_key, false)

  def destination(subject) do
    case subject do
      "twitch.ingress.event.premium" -> subject
      "twitch.ingress.event.standard" -> subject
      "twitch.ingress.event.stream" -> subject
      _other -> "twitch.ingress.event.other"
    end
  end

  def result(:ok), do: "ok"
  def result({:error, :timeout}), do: "timeout"
  def result({:error, _reason}), do: "error"
  def result(:squash), do: "filtered"
  def result(:oversized), do: "invalid"
  def result(:drop), do: "dropped"
  def result(_other), do: "ok"
end
