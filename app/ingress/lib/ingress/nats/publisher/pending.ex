# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary and unlicensed. See LICENSE.md.

defmodule Ingress.Nats.Publisher.Pending do
  @moduledoc """
  In-flight bookkeeping for one publisher shard: the pending-publish ETS table
  and the `:atomics` counter shared with the scheduler-local admission path.

  Every row shape and every counter slot is spelled out here once, so a wire
  implementation never carries an index and admission never reaches into the
  table. A row is one publish awaiting its own PubAck, a whole atomic cohort
  awaiting one commit ack, or the empty slot a swept cohort leaves behind:

      {id, :single,     subject, payload, attempts, monotonic_ms}
      {id, :batch,      entries, monotonic_ms}
      {id, :batch_hold, monotonic_ms}

  `payload` is the bare `iodata()` body, or `{body, trace_headers}` for the
  sparse traced events — the unsampled firehose never allocates the wider
  tuple. No shape has a dedup-id slot: ingress publishes without
  `Nats-Msg-Id` by design, and the row shape keeps it that way.

  `:batch_hold` carries no events. Its only job is to keep one atomic-batch
  in-flight slot charged after the cohort itself has been resolved, because
  nothing on the wire cancels an open batch and the broker keeps holding it;
  it carries the ORIGINAL open stamp so it retires on the broker's clock, not
  on a fresh one. See `Ingress.Nats.Publisher.Wire.Atomic`.
  """

  @idx_pending 1
  @idx_next_id 2
  @idx_acked 3
  @idx_retried 4
  @idx_failed 5
  @idx_cohorts 6
  @idx_batch_inflight 7
  @idx_batch_fallback 8
  @idx_batch_bypass 9
  @idx_batch_headers_ignored 10
  @slots 10

  @type table :: :ets.table()
  @type counter :: :atomics.atomics_ref()
  @type id :: pos_integer()
  @type row :: tuple()

  ## Shard-local stores

  @spec new_table(non_neg_integer()) :: table()
  def new_table(index) do
    :ets.new(:"ingress_pub_pending_#{index}", [
      :named_table,
      :public,
      :set,
      read_concurrency: true,
      write_concurrency: true
    ])
  end

  @spec new_counter() :: counter()
  def new_counter, do: :atomics.new(@slots, signed: false)

  @spec now_ms() :: integer()
  def now_ms, do: System.monotonic_time(:millisecond)

  ## Admission window

  # Claims one slot of the in-flight window and reports the resulting depth;
  # the caller releases it again when the claim turns out to be inadmissible.
  @spec reserve(counter()) :: non_neg_integer()
  def reserve(counter), do: :atomics.add_get(counter, @idx_pending, 1)

  @spec release(counter()) :: :ok
  def release(counter), do: :atomics.sub(counter, @idx_pending, 1)

  @spec settle(counter(), pos_integer()) :: :ok
  def settle(counter, count), do: :atomics.sub(counter, @idx_pending, count)

  @spec pending(counter()) :: non_neg_integer()
  def pending(counter), do: :atomics.get(counter, @idx_pending)

  @spec next_id(counter()) :: id()
  def next_id(counter), do: :atomics.add_get(counter, @idx_next_id, 1)

  ## Rows

  @spec insert_single(table(), id(), String.t(), term(), pos_integer()) :: true
  def insert_single(table, id, subject, payload, attempts),
    do: :ets.insert(table, {id, :single, subject, payload, attempts, now_ms()})

  @spec insert_batch(table(), id(), [term()]) :: true
  def insert_batch(table, id, entries),
    do: :ets.insert(table, {id, :batch, entries, now_ms()})

  # Replaces a resolved cohort with the slot it still owes the broker. The
  # stamp is the batch's original open stamp, never a fresh one.
  @spec insert_batch_hold(table(), id(), integer()) :: true
  def insert_batch_hold(table, id, stamp), do: :ets.insert(table, {id, :batch_hold, stamp})

  @spec lookup(table(), id()) :: [row()]
  def lookup(table, id), do: :ets.lookup(table, id)

  @spec delete(table(), id()) :: true
  def delete(table, id), do: :ets.delete(table, id)

  # Sweep scan: every row past the deadline that applies to its shape. Rows
  # carrying live events use the publisher's ack deadline; a `:batch_hold`
  # carries no events and retires on the broker's later batch deadline. The
  # caller routes each one to the wire that owns it.
  @spec expired(table(), integer(), integer()) :: [row()]
  def expired(table, ack_deadline, batch_deadline) do
    table
    |> :ets.tab2list()
    |> Enum.filter(&expired?(&1, ack_deadline, batch_deadline))
  end

  defp expired?({_id, :single, _subject, _payload, _attempts, stamp}, ack_deadline, _batch),
    do: stamp <= ack_deadline

  defp expired?({_id, :batch, _entries, stamp}, ack_deadline, _batch), do: stamp <= ack_deadline
  defp expired?({_id, :batch_hold, stamp}, _ack, batch_deadline), do: stamp <= batch_deadline
  defp expired?(_row, _ack_deadline, _batch_deadline), do: false

  ## Outcome counters
  #
  # Wires bump these; the collector's gauge tick drains them into metrics.

  @spec acked(counter(), pos_integer()) :: :ok
  def acked(counter, count), do: :atomics.add(counter, @idx_acked, count)

  @spec failed(counter(), pos_integer()) :: :ok
  def failed(counter, count), do: :atomics.add(counter, @idx_failed, count)

  @spec retried(counter()) :: :ok
  def retried(counter), do: :atomics.add(counter, @idx_retried, 1)

  @spec cohort(counter()) :: :ok
  def cohort(counter), do: :atomics.add(counter, @idx_cohorts, 1)

  ## Atomic-batch budget

  @spec batch_opened(counter()) :: :ok
  def batch_opened(counter), do: :atomics.add(counter, @idx_batch_inflight, 1)

  @spec batch_closed(counter()) :: :ok
  def batch_closed(counter), do: :atomics.sub(counter, @idx_batch_inflight, 1)

  @spec batches_inflight(counter()) :: non_neg_integer()
  def batches_inflight(counter), do: :atomics.get(counter, @idx_batch_inflight)

  @spec batch_fallback(counter()) :: :ok
  def batch_fallback(counter), do: :atomics.add(counter, @idx_batch_fallback, 1)

  @spec batch_bypassed(counter()) :: :ok
  def batch_bypassed(counter), do: :atomics.add(counter, @idx_batch_bypass, 1)

  # Cohorts the broker stored as ordinary publishes because it never
  # interpreted the Nats-Batch-* headers. Kept apart from every failure
  # counter: the events ARE on the stream.
  @spec batch_headers_ignored(counter()) :: :ok
  def batch_headers_ignored(counter), do: :atomics.add(counter, @idx_batch_headers_ignored, 1)

  ## Gauge drains

  @spec take_acked(counter()) :: non_neg_integer()
  def take_acked(counter), do: take(counter, @idx_acked)

  @spec take_retried(counter()) :: non_neg_integer()
  def take_retried(counter), do: take(counter, @idx_retried)

  @spec take_failed(counter()) :: non_neg_integer()
  def take_failed(counter), do: take(counter, @idx_failed)

  @spec take_cohorts(counter()) :: non_neg_integer()
  def take_cohorts(counter), do: take(counter, @idx_cohorts)

  @spec take_batch_fallback(counter()) :: non_neg_integer()
  def take_batch_fallback(counter), do: take(counter, @idx_batch_fallback)

  @spec take_batch_bypassed(counter()) :: non_neg_integer()
  def take_batch_bypassed(counter), do: take(counter, @idx_batch_bypass)

  @spec take_batch_headers_ignored(counter()) :: non_neg_integer()
  def take_batch_headers_ignored(counter), do: take(counter, @idx_batch_headers_ignored)

  defp take(counter, index), do: :atomics.exchange(counter, index, 0)
end
