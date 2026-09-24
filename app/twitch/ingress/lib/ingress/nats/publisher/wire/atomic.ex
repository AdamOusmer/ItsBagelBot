# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats.Publisher.Wire.Atomic do
  @behaviour Ingress.Nats.Publisher.Wire

  require Logger

  alias Ingress.Nats.Publisher.{AckPath, Pending, Wire}
  alias Ingress.Nats.Publisher.Wire.Single

  @impl Wire
  def send_cohort(entries, %Wire{} = wire) do
    id = Pending.next_id(wire.counter)
    batch_id = wire.batch_token <> "-" <> Integer.to_string(id)
    Pending.insert_batch(wire.table, id, entries)
    Pending.batch_opened(wire.counter)

    case write(entries, batch_id, id, wire) do
      :ok ->
        :ok

      # The commit may have reached the socket: a replay could double-store the cohort.
      {:error, _reason} ->
        fail(id, entries, wire)
    end
  end

  defp write(entries, batch_id, id, wire) do
    last = length(entries)

    entries
    |> Enum.with_index(1)
    |> Enum.reduce_while(:ok, fn {entry, seq}, :ok ->
      {subject, json, trace_headers} = Wire.parts(entry)
      headers = batch_headers(batch_id, seq, last) ++ trace_headers
      opts = pub_opts(headers, reply_for(seq, last, id, wire))

      case Wire.safe_pub(wire.conn, {subject, json, opts}, wire.call_timeout_ms) do
        :ok -> {:cont, :ok}
        {:error, reason} -> {:halt, {:error, reason}}
      end
    end)
  end

  defp batch_headers(batch_id, seq, last) do
    commit = if seq == last, do: [{"Nats-Batch-Commit", "1"}], else: []

    [{"Nats-Batch-Id", batch_id}, {"Nats-Batch-Sequence", Integer.to_string(seq)}] ++
      commit
  end

  defp reply_for(1, last, id, wire) when last > 1, do: AckPath.batch_start(wire.prefix, id)
  defp reply_for(seq, last, id, wire) when seq == last, do: AckPath.batch_commit(wire.prefix, id)
  defp reply_for(_seq, _last, _id, _wire), do: nil

  defp pub_opts(headers, nil), do: [headers: headers]
  defp pub_opts(headers, reply), do: [reply_to: reply, headers: headers]

  @impl Wire
  def ack({:batch_start, _id}, "", %Wire{}), do: :ok

  def ack({:batch_start, id}, body, %Wire{} = wire),
    do: with_row(id, wire, &opened(Wire.classify(body), &1, wire))

  def ack({:batch_commit, id}, body, %Wire{} = wire),
    do: with_row(id, wire, &committed(Wire.classify(body), &1, wire))

  @impl Wire
  def expire({_id, :batch, _entries, _stamp} = row, %Wire{} = wire), do: abandon(row, wire)
  def expire({id, :batch_hold, _stamp}, %Wire{} = wire), do: release_hold(id, wire)

  defp with_row(id, wire, resolve) do
    case Pending.lookup(wire.table, id) do
      [{^id, :batch, _entries, _stamp} = row] ->
        resolve.(row)

      [{^id, :batch_hold, _stamp}] ->
        release_hold(id, wire)

      _ ->
        :ok
    end
  end

  defp opened(:ok, row, wire) do
    log_headers_ignored()
    Pending.batch_headers_ignored(wire.counter)
    resolve(row, wire)
  end

  defp opened(:rejected, row, wire), do: fallback(row, wire)
  defp opened(_ambiguous, row, wire), do: fail(row, wire)

  defp committed(:ok, row, wire), do: resolve(row, wire)
  defp committed(:rejected, row, wire), do: fallback(row, wire)
  defp committed(:ambiguous, row, wire), do: fail(row, wire)

  defp log_headers_ignored do
    if is_nil(Process.put(:ingress_batch_headers_ignored, true)) do
      Logger.warning(
        "nats atomic batch headers ignored: the batch-open message was answered with a " <>
          "stored PubAck, so this broker does not implement ADR-050 atomic publish. " <>
          "Cohorts are landing as ordinary publishes; set INGRESS_PUBLISH_WIRE=single " <>
          "to stop paying for batch headers."
      )
    end

    :ok
  end

  defp fallback({id, :batch, entries, _stamp}, wire) do
    Pending.delete(wire.table, id)
    Pending.batch_closed(wire.counter)
    Pending.batch_fallback(wire.counter)
    Single.send_cohort(entries, wire)
  end

  defp resolve({id, :batch, entries, stamp}, wire) do
    count = length(entries)
    Pending.resolve(wire.table, wire.counter, id, count)
    Wire.record_ack_latency(id, stamp, count)
    Pending.batch_closed(wire.counter)
  end

  defp fail({id, :batch, entries, _stamp}, wire), do: fail(id, entries, wire)

  defp fail(id, entries, wire) do
    settle_failed(id, entries, wire)
    Pending.batch_closed(wire.counter)
  end

  defp abandon({id, :batch, entries, stamp}, wire) do
    settle_failed(id, entries, wire)
    Pending.insert_batch_hold(wire.table, id, stamp)
    :ok
  end

  defp release_hold(id, wire) do
    Pending.delete(wire.table, id)
    Pending.batch_closed(wire.counter)
  end

  defp settle_failed(id, entries, wire),
    do: Pending.fail(wire.table, wire.counter, id, length(entries))
end
