# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Trace do
  @moduledoc false

  alias Ingress.Config

  # Wire contract with pkg/bus/ingress_trace.go; both sides change together.
  @header "bagel-ingress"
  @version "v1"
  @context_key {__MODULE__, :context}
  @message_id ~r/\A[0-9A-Za-z_.:+\/=-]{1,96}\z/

  def notification(enqueued_at, _payload, meta, fun) when is_function(fun, 0) do
    now = System.monotonic_time()

    Process.put(@context_key, %{
      trace_id: random_hex(16),
      span_id: random_hex(8),
      started: now,
      started_us: System.os_time(:microsecond),
      wait_us: micros(max(now - enqueued_at, 0)),
      message_id: message_id(Map.get(meta, :msg_id)),
      steps: []
    })

    try do
      fun.()
    after
      Process.delete(@context_key)
    end
  end

  def trace_headers do
    case Process.get(@context_key) do
      nil -> []
      context -> headers(context)
    end
  end

  def hold do
    case Process.get(@context_key) do
      nil -> nil
      context -> Map.put(context, :held_at, System.monotonic_time())
    end
  end

  def held_headers(nil), do: []

  def held_headers(%{held_at: held_at, started: started} = context) do
    step = {"squash", micros(held_at - started), micros(System.monotonic_time() - held_at)}
    headers(%{context | steps: [step | context.steps]})
  end

  defp headers(context),
    do: [{"traceparent", traceparent(context)}, {@header, header_value(context)}]

  def headers_for(subject, headers) do
    if chat_lane?(subject), do: headers || trace_headers(), else: []
  end

  def span(name, fun) when is_function(fun, 0) do
    case Process.get(@context_key) do
      nil -> fun.()
      context -> record_step(name, context.started, fun)
    end
  end

  defp record_step(name, started, fun) do
    step_start = System.monotonic_time()

    try do
      fun.()
    after
      step = {name, micros(step_start - started), micros(System.monotonic_time() - step_start)}

      with %{steps: steps} = context <- Process.get(@context_key) do
        Process.put(@context_key, %{context | steps: [step | steps]})
      end
    end
  end

  defp chat_lane?(subject),
    do: subject in [Config.lane_subject(:premium), Config.lane_subject(:standard)]

  defp traceparent(context), do: "00-#{context.trace_id}-#{context.span_id}-01"

  defp header_value(context) do
    ended_us = context.started_us + micros(System.monotonic_time() - context.started)

    steps =
      context.steps
      |> Enum.reverse()
      |> Enum.map_join("|", fn {name, offset, duration} -> "#{name},#{offset},#{duration}" end)

    Enum.join(
      [@version, context.started_us, ended_us, context.wait_us, context.message_id, steps],
      ";"
    )
  end

  defp message_id(id) when is_binary(id) do
    if Regex.match?(@message_id, id), do: id, else: ""
  end

  defp message_id(_id), do: ""

  defp micros(native), do: System.convert_time_unit(native, :native, :microsecond)

  defp random_hex(bytes), do: bytes |> :crypto.strong_rand_bytes() |> Base.encode16(case: :lower)
end
