# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats.Publisher.Pending do
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

  @spec insert_single(table(), id(), String.t(), term(), pos_integer()) :: true
  def insert_single(table, id, subject, payload, attempts),
    do: :ets.insert(table, {id, :single, subject, payload, attempts, now_ms()})

  @spec insert_batch(table(), id(), [term()]) :: true
  def insert_batch(table, id, entries),
    do: :ets.insert(table, {id, :batch, entries, now_ms()})

  @spec insert_batch_hold(table(), id(), integer()) :: true
  def insert_batch_hold(table, id, stamp), do: :ets.insert(table, {id, :batch_hold, stamp})

  @spec lookup(table(), id()) :: [row()]
  def lookup(table, id), do: :ets.lookup(table, id)

  @spec delete(table(), id()) :: true
  def delete(table, id), do: :ets.delete(table, id)

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

  # Settle via resolve/fail only: deleting a row without releasing its slot leaks the slot.
  @spec resolve(table(), counter(), id(), pos_integer()) :: :ok
  def resolve(table, counter, id, count) do
    delete(table, id)
    settle(counter, count)
    acked(counter, count)
  end

  @spec fail(table(), counter(), id(), pos_integer()) :: :ok
  def fail(table, counter, id, count) do
    delete(table, id)
    settle(counter, count)
    failed(counter, count)
  end

  @spec acked(counter(), pos_integer()) :: :ok
  def acked(counter, count), do: :atomics.add(counter, @idx_acked, count)

  @spec failed(counter(), pos_integer()) :: :ok
  def failed(counter, count), do: :atomics.add(counter, @idx_failed, count)

  @spec retried(counter()) :: :ok
  def retried(counter), do: :atomics.add(counter, @idx_retried, 1)

  @spec cohort(counter()) :: :ok
  def cohort(counter), do: :atomics.add(counter, @idx_cohorts, 1)

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

  @spec batch_headers_ignored(counter()) :: :ok
  def batch_headers_ignored(counter), do: :atomics.add(counter, @idx_batch_headers_ignored, 1)

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
