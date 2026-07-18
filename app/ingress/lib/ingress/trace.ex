defmodule Ingress.Trace do
  @moduledoc false

  # Runs exactly one notification in a short-lived transaction on the worker
  # process. The dispatcher timestamp uses the same monotonic clock, so queue
  # wait remains meaningful across scheduler migration without wall-clock skew.
  alias Ingress.Config

  @active_key {__MODULE__, :active}

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

    NewRelic.start_transaction("Ingress", "Notification")
    Process.put(@active_key, true)

    NewRelic.add_attributes(
      "dispatcher.wait_ms": wait_us / 1_000,
      "event.type": event_type(payload)
    )

    try do
      fun.()
    after
      Process.delete(@active_key)
      NewRelic.stop_transaction()
    end
  end

  defp sampled?(meta) do
    case Config.trace_sample_rate() do
      rate when rate <= 0 -> false
      1 -> true
      rate -> :erlang.phash2({Map.get(meta, :msg_id), Map.get(meta, :shard_id)}, rate) == 0
    end
  end

  defp event_type(%{"subscription" => %{"type" => type}}) when is_binary(type), do: type
  defp event_type(_payload), do: "invalid"

  def trace_headers do
    if active?() do
      :other
      |> NewRelic.distributed_trace_headers()
      |> Map.to_list()
    else
      []
    end
  end

  def span(name, attributes \\ [], fun) when is_function(fun, 0) do
    if active?() do
      id = make_ref()
      NewRelic.Tracer.Direct.start_span(id, name, attributes: attributes)

      try do
        fun.()
      after
        NewRelic.Tracer.Direct.stop_span(id)
      end
    else
      fun.()
    end
  end

  def add_span_attributes(attributes) do
    if active?(), do: NewRelic.add_span_attributes(attributes)
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
