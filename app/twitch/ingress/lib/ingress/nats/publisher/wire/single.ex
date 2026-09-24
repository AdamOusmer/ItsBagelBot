# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats.Publisher.Wire.Single do
  @behaviour Ingress.Nats.Publisher.Wire

  alias Ingress.Nats.CohortSender
  alias Ingress.Nats.Publisher.{AckPath, Pending, Wire}

  @impl Wire
  def send_cohort(entries, %Wire{} = wire) do
    requests = Enum.map(entries, &stage(&1, wire))

    wire.senders
    |> CohortSender.publish(wire.conn, requests, wire.call_timeout_ms)
    |> Enum.each(&finish_send(&1, wire))
  end

  defp stage({subject, json, from}, wire) do
    id = Pending.next_id(wire.counter)
    Pending.insert_single(wire.table, id, subject, json, 1)

    {{id, from}, subject, json, [reply_to: AckPath.single(wire.prefix, id)]}
  end

  defp stage({subject, json, trace_headers, from}, wire) do
    id = Pending.next_id(wire.counter)
    Pending.insert_single(wire.table, id, subject, {json, trace_headers}, 1)

    opts = Wire.publish_opts([reply_to: AckPath.single(wire.prefix, id)], trace_headers)
    {{id, from}, subject, json, opts}
  end

  defp finish_send({{_id, from}, :ok}, _wire), do: Wire.reply(from, :ok)

  defp finish_send({{id, from}, {:error, reason}}, wire) do
    settle_failed(id, wire)
    Wire.reply(from, {:error, reason})
  end

  @impl Wire
  def ack({:single, id}, body, %Wire{} = wire) do
    case Pending.lookup(wire.table, id) do
      [{^id, :single, _subject, _payload, _attempts, _stamp} = row] ->
        settle(Wire.classify(body), row, wire)

      [] ->
        :ok
    end
  end

  # The broker may have stored it; without Nats-Msg-Id, drop rather than duplicate.
  @impl Wire
  def expire(row, %Wire{} = wire), do: drop(row, wire)

  defp settle(:ok, {id, :single, _subject, _payload, _attempts, stamp}, wire),
    do: resolve(id, stamp, wire)

  defp settle(:rejected, row, wire), do: retry_or_drop(row, wire)

  defp settle(:ambiguous, row, wire), do: drop(row, wire)

  defp retry_or_drop({_id, :single, _subject, _payload, attempts, _stamp} = row, wire) do
    if attempts < wire.max_attempts do
      retry(row, wire)
    else
      drop(row, wire)
    end
  end

  defp retry({id, :single, subject, payload, attempts, _stamp}, wire) do
    Pending.insert_single(wire.table, id, subject, payload, attempts + 1)
    {json, trace_headers} = payload_parts(payload)
    opts = Wire.publish_opts([reply_to: AckPath.single(wire.prefix, id)], trace_headers)

    case Wire.safe_pub(wire.conn, {subject, json, opts}, wire.call_timeout_ms) do
      :ok -> Pending.retried(wire.counter)
      {:error, _reason} -> settle_failed(id, wire)
    end
  end

  defp payload_parts({json, trace_headers}), do: {json, trace_headers}
  defp payload_parts(json), do: {json, []}

  defp resolve(id, stamp, wire) do
    Wire.record_ack_latency(id, stamp, 1)
    Pending.resolve(wire.table, wire.counter, id, 1)
  end

  defp drop({id, :single, _subject, _payload, _attempts, _stamp}, wire),
    do: settle_failed(id, wire)

  defp settle_failed(id, wire), do: Pending.fail(wire.table, wire.counter, id, 1)
end
