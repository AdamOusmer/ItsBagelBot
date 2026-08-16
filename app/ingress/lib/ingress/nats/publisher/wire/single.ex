# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats.Publisher.Wire.Single do
  @moduledoc """
  Per-event wire: every member of the cohort is an ordinary JetStream publish
  with its own reply inbox and its own PubAck.

  This is the long-standing ingress path and the compatibility fallback for the
  atomic wire — a definitely rejected batch is re-driven through here, because
  a negative acknowledgement proves the broker stored none of it. The cohort
  only amortizes collector scheduling: the writes still fan across the shard's
  `CohortSender` lanes so Gnat can coalesce them into fewer socket writes.

  Ingress attaches no `Nats-Msg-Id`, so an ack timeout or a malformed PubAck
  drops the event rather than risking a double-store; only a definite negative
  PubAck retries, within the shard's attempt budget.
  """

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

  # An ack timeout is ambiguous: the broker may have stored the event and only
  # the ack was lost. Without Nats-Msg-Id the event is dropped rather than
  # risking a duplicate.
  @impl Wire
  def expire(row, %Wire{} = wire), do: drop(row, wire)

  defp settle(:ok, {id, :single, _subject, _payload, _attempts, stamp}, wire),
    do: resolve(id, stamp, wire)

  # A definite negative PubAck means nothing was stored, so a replay cannot
  # double-store and may be retried within the bounded attempt budget.
  defp settle(:rejected, row, wire), do: retry_or_drop(row, wire)

  # Malformed acknowledgements are ambiguous and therefore fail closed too.
  defp settle(:ambiguous, row, wire), do: drop(row, wire)

  defp retry_or_drop({_id, :single, _subject, _payload, attempts, _stamp} = row, wire) do
    if attempts < wire.max_attempts do
      retry(row, wire)
    else
      drop(row, wire)
    end
  end

  # The row is re-inserted with a fresh stamp before the write, so a PubAck
  # racing the call still finds it. If the write never leaves the VM the
  # publisher already knows the outcome, so settle it here: holding the slot
  # for another full ack timeout would keep the shard's admission window
  # saturated for attempts × ack_timeout after the connection is already gone,
  # shedding new events as :overloaded instead of the accurate :not_connected —
  # and would count a retry that never happened.
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
    Pending.delete(wire.table, id)
    Pending.settle(wire.counter, 1)
    Pending.acked(wire.counter, 1)
  end

  defp drop({id, :single, _subject, _payload, _attempts, _stamp}, wire),
    do: settle_failed(id, wire)

  defp settle_failed(id, wire) do
    Pending.delete(wire.table, id)
    Pending.settle(wire.counter, 1)
    Pending.failed(wire.counter, 1)
  end
end
