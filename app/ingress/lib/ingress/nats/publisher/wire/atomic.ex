defmodule Ingress.Nats.Publisher.Wire.Atomic do
  @moduledoc """
  Cohort wire: one flushed cohort travels as a single ADR-050 atomic batch
  (NATS 2.14).

  Messages carry sequenced `Nats-Batch-*` headers. The opening message carries
  a reply so a rejected open surfaces at once, the intermediates are
  unacknowledged, and the final message commits the batch into one PubAck. The
  whole cohort is one pending row until that commit ack, an error reply, or the
  sweep deadline resolves it — so an R3 stream pays RAFT quorum latency once
  per cohort instead of once per event.

  Resolution follows the wire contract's dedup-free rule. A definite server
  rejection is re-driven per message through `Wire.Single`, because the broker
  stored nothing. A send error, a malformed reply or a missing commit ack is
  ambiguous and drops the cohort rather than risking a double-store.

  ## Blast radius

  One pending row per cohort is exactly what makes this wire cheap and exactly
  what makes a bad outcome expensive: an ambiguous resolution drops every event
  in the cohort — up to `INGRESS_PUBLISH_BATCH_SIZE` = 128 — where the same
  outcome on `Wire.Single` costs 1. Every dropped event is still counted, so
  the signal an operator watches is the SHAPE of `Nats/PublishFailed`: on this
  wire it moves in cohort-sized steps, on `Wire.Single` one event at a time.
  Step-shaped growth is the cue to fall back with
  `INGRESS_PUBLISH_WIRE=single`, which buys per-event granularity back for one
  RAFT quorum round trip per event.

  ## The broker's clock outlives ours

  Nothing on the wire cancels an open batch. Once the sweep resolves a cohort
  the broker still holds the abandoned batch — its per-stream in-flight slot,
  and its staged cohort in the R3 leader's RAM — until its own inactivity timer
  fires (`jetstream.limits.batch.timeout`, default 10s). So a swept batch does
  not free its local slot; it leaves a `:batch_hold` row that retires on the
  broker's deadline, and shard admission stays inside the broker's budget
  instead of recycling ~5× through it while the broker is already stalled.
  """

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

      # A local send failure can be ambiguous when the commit write reached the
      # socket but its call result did not. Without message IDs a per-message
      # replay could double-store the entire cohort, so fail closed.
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

      case Wire.safe_pub(wire.conn, subject, json, opts, wire.call_timeout_ms) do
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

  # Only the opening message (so a rejected open surfaces immediately) and the
  # committing message (which carries the cohort's PubAck) get a reply inbox.
  defp reply_for(1, last, id, wire) when last > 1, do: AckPath.batch_start(wire.prefix, id)
  defp reply_for(seq, last, id, wire) when seq == last, do: AckPath.batch_commit(wire.prefix, id)
  defp reply_for(_seq, _last, _id, _wire), do: nil

  defp pub_opts(headers, nil), do: [headers: headers]
  defp pub_opts(headers, reply), do: [reply_to: reply, headers: headers]

  # Batch-open reply: zero-byte means the broker accepted the batch and the
  # commit ack will resolve it. Only an explicit negative PubAck proves a
  # rejection and permits fallback; malformed replies fail closed.
  @impl Wire
  def ack({:batch_start, _id}, "", %Wire{}), do: :ok

  def ack({:batch_start, id}, body, %Wire{} = wire),
    do: with_row(id, wire, &opened(Wire.classify(body), &1, wire))

  def ack({:batch_commit, id}, body, %Wire{} = wire),
    do: with_row(id, wire, &committed(Wire.classify(body), &1, wire))

  # A swept batch is the cohort-shaped ack timeout: the commit may have landed
  # with only its ack lost. Dedup is deliberately unavailable, so drop the
  # whole cohort rather than risking a double-store. Error replies never come
  # here — definite rejections take fallback/2 directly.
  #
  # The second clause is the broker's half of the same sweep: a `:batch_hold`
  # row carries no events, only the in-flight slot the broker is still holding,
  # and it expires on the broker's batch deadline rather than the ack deadline.
  @impl Wire
  def expire({_id, :batch, _entries, _stamp} = row, %Wire{} = wire), do: abandon(row, wire)
  def expire({id, :batch_hold, _stamp}, %Wire{} = wire), do: release_hold(id, wire)

  defp with_row(id, wire, resolve) do
    case Pending.lookup(wire.table, id) do
      [{^id, :batch, _entries, _stamp} = row] ->
        resolve.(row)

      # A reply for a batch this shard already swept: the broker has finished
      # with it after all, so hand the held slot back now instead of waiting
      # out a deadline that has already been proven moot. The events stay
      # resolved — the dedup-free rule dropped them and cannot un-drop them.
      [{^id, :batch_hold, _stamp}] ->
        release_hold(id, wire)

      _ ->
        :ok
    end
  end

  # A success PubAck on the batch-OPEN inbox has exactly one meaning: the
  # broker never interpreted `Nats-Batch-*` and stored the message as an
  # ordinary publish. A 2.14 broker answers a staged message with a zero-byte
  # ack, and answers every rejection — including allow_atomic being off, which
  # dispatches on the header rather than the stream flag — with an error
  # PubAck. So message 1 is stored, and so are 2..N (they were published
  # without replies) and the commit message. Resolving them as failures, which
  # is what the ambiguous catch-all used to do, reports a 100% publish-failure
  # rate during perfectly healthy ingest on a pre-2.14 broker.
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

  # Once per collector process, not once per cohort: the condition is a fact
  # about the broker on the other end of this connection, so every cohort would
  # repeat it at firehose rate. Nats/PublishBatchHeadersIgnored carries the
  # volume; this line carries the explanation.
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

  # Re-drives a definitely rejected batch per message over the single wire. A
  # negative server acknowledgement proves the atomic cohort was not committed;
  # ambiguous transport failures and timeouts use fail/2 instead.
  defp fallback({id, :batch, entries, _stamp}, wire) do
    Pending.delete(wire.table, id)
    Pending.batch_closed(wire.counter)
    Pending.batch_fallback(wire.counter)
    Single.send_cohort(entries, wire)
  end

  defp resolve({id, :batch, entries, stamp}, wire) do
    Pending.delete(wire.table, id)
    count = length(entries)
    Wire.record_ack_latency(id, stamp, count)
    Pending.settle(wire.counter, count)
    Pending.acked(wire.counter, count)
    Pending.batch_closed(wire.counter)
  end

  defp fail({id, :batch, entries, _stamp}, wire), do: fail(id, entries, wire)

  defp fail(id, entries, wire) do
    settle_failed(id, entries, wire)
    Pending.batch_closed(wire.counter)
  end

  # The sweep's own resolution. The events drop like any other ambiguous
  # outcome, but the in-flight slot does NOT come back: no wire message cancels
  # an open batch, so the broker keeps this batch's per-stream slot and its
  # staged cohort until its inactivity timer fires. The hold row carries the
  # ORIGINAL open stamp, so it retires on the broker's clock (batch_hold_ms)
  # and local admission can never open more batches than the broker is holding.
  defp abandon({id, :batch, entries, stamp}, wire) do
    settle_failed(id, entries, wire)
    Pending.insert_batch_hold(wire.table, id, stamp)
    :ok
  end

  defp release_hold(id, wire) do
    Pending.delete(wire.table, id)
    Pending.batch_closed(wire.counter)
  end

  defp settle_failed(id, entries, wire) do
    Pending.delete(wire.table, id)
    count = length(entries)
    Pending.settle(wire.counter, count)
    Pending.failed(wire.counter, count)
  end
end
