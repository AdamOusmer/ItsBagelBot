# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats.Publisher do
  @moduledoc """
  One scheduler-local, bounded JetStream cohort publisher.

  Calls arriving within `publish_batch_wait_ms` are staged as a local cohort —
  one per subject, because a cohort is a per-stream object and the lanes span
  two streams (see `flush_subject/2`) — then handed to the wire selected by
  `Ingress.Config.Publish.wire/0`:

    * `:atomic` (default) — the cohort is written as one ADR-050 atomic batch
      (NATS 2.14) and resolved by a single commit PubAck, so a replicated
      stream pays RAFT quorum latency once per cohort instead of once per
      event.
    * `:single` — one ordinary JetStream PubAck per event; the compatibility
      fallback, and where a definitely rejected batch is re-driven.

  Neither wire attaches `Nats-Msg-Id`: EventSub websockets do not replay and
  the broker dedup index materially reduces ingest throughput. Every ambiguous
  outcome therefore drops instead of retrying; only definite negative PubAcks
  retry. See `Ingress.Nats.Publisher.Wire` for the full contract and for the
  deferred Fast-Ingest seam.

  This module owns the process: lifecycle, admission, cohort assembly and flush
  scheduling, the ack subscription, sweeping and metrics. Wires are plain
  functions it calls; `Ingress.Nats.Publisher.Pending` owns the in-flight row
  and counter shapes, and `Ingress.Nats.Publisher.AckPath` owns the reply-inbox
  naming.

  `Ingress.Nats.PublisherPool` runs one publisher and BUS connection per online
  BEAM scheduler. Admission and cohort assembly are serialized only inside that local
  shard, with bounded fallback probing when a shard is full or disconnected.
  """

  use GenServer

  require Logger

  alias Ingress.Config.Publish, as: PublishConfig
  alias Ingress.Metrics
  alias Ingress.Nats.CohortSender
  alias Ingress.Nats.Publisher.{AckPath, Pending, Wire}
  alias Ingress.Nats.Publisher.Wire.{Atomic, Single}

  @sweep_interval_ms 500
  @gauge_interval_ms 5_000

  ## Scheduler-local admission

  @spec enqueue(String.t(), iodata(), Gnat.headers()) :: :ok | {:error, term()}
  def enqueue(subject, json, trace_headers \\ []) do
    case :persistent_term.get({__MODULE__, :n}, 0) do
      0 ->
        {:error, :not_connected}

      n ->
        index = rem(max(:erlang.system_info(:scheduler_id), 1) - 1, n)
        enqueue_from(index, n, subject, json, trace_headers, nil)
    end
  end

  defp enqueue_from(_index, 0, _subject, _json, _headers, nil),
    do: {:error, :not_connected}

  defp enqueue_from(_index, 0, _subject, _json, _headers, last_error), do: last_error

  defp enqueue_from(index, remaining, subject, json, trace_headers, _last_error) do
    n = :persistent_term.get({__MODULE__, :n})

    result =
      case :persistent_term.get({__MODULE__, :ctx, index}, nil) do
        nil -> {:error, :not_connected}
        ctx -> admit(ctx, subject, json, trace_headers)
      end

    case result do
      :ok ->
        :ok

      {:error, reason} = error when reason in [:overloaded, :not_connected] ->
        enqueue_from(rem(index + 1, n), remaining - 1, subject, json, trace_headers, error)
    end
  end

  defp admit(
         %{pid: pid, conn: conn, counter: counter, max_pending: max_pending},
         subject,
         json,
         trace_headers
       ) do
    cond do
      Pending.reserve(counter) > max_pending ->
        Pending.release(counter)
        {:error, :overloaded}

      not Process.alive?(pid) or is_nil(Process.whereis(conn)) ->
        Pending.release(counter)
        {:error, :not_connected}

      true ->
        enqueue_cast(pid, subject, json, trace_headers)
        :ok
    end
  end

  # Preserve the allocation profile of the unsampled firehose. Only the sparse
  # traced messages carry the wider tuple and header list through the cohort.
  defp enqueue_cast(pid, subject, json, []), do: GenServer.cast(pid, {:enqueue, subject, json})

  defp enqueue_cast(pid, subject, json, trace_headers),
    do: GenServer.cast(pid, {:enqueue, subject, json, trace_headers})

  ## Collector lifecycle

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    index = Keyword.fetch!(opts, :index)
    GenServer.start_link(__MODULE__, opts, name: process_name(index))
  end

  @doc false
  def process_name(index), do: :"#{__MODULE__}.#{index}"

  @impl true
  def init(opts) do
    index = Keyword.fetch!(opts, :index)
    {token, prefix} = AckPath.new_inbox()

    wire = %Wire{
      conn: Keyword.fetch!(opts, :conn),
      table: Pending.new_table(index),
      counter: Pending.new_counter(),
      prefix: prefix,
      batch_token: token,
      senders: CohortSender.start(PublishConfig.send_concurrency()),
      max_attempts: PublishConfig.attempts(),
      call_timeout_ms: PublishConfig.call_timeout_ms()
    }

    state = initial_state(index, wire)
    publish_context(state)

    send(self(), :connect)
    schedule(:sweep, @sweep_interval_ms)
    schedule(:gauge, @gauge_interval_ms)
    {:ok, state}
  end

  defp initial_state(index, %Wire{} = wire) do
    %{
      index: index,
      wire: wire,
      sub_topic: AckPath.subscription(wire.prefix),
      sid: nil,
      conn_ref: nil,
      max_pending: PublishConfig.max_pending(),
      ack_timeout_ms: PublishConfig.ack_timeout_ms(),
      batch_hold_ms: PublishConfig.batch_hold_ms(),
      batch_size: PublishConfig.batch_size(),
      batch_wait_ms: PublishConfig.batch_wait_ms(),
      wire_mode: PublishConfig.wire(),
      batch_inflight_cap: PublishConfig.batch_inflight(),
      queues: %{},
      queue_count: 0,
      flush_token: nil
    }
  end

  # The admission context is read from any scheduler without touching this
  # process, so it carries only immutable handles.
  defp publish_context(state) do
    :persistent_term.put(
      {__MODULE__, :ctx, state.index},
      %{
        pid: self(),
        counter: state.wire.counter,
        prefix: state.wire.prefix,
        table: state.wire.table,
        conn: state.wire.conn,
        max_pending: state.max_pending
      }
    )
  end

  @impl true
  def handle_cast({:enqueue, subject, json}, state) do
    enqueue_entry({subject, json, nil}, state)
  end

  def handle_cast({:enqueue, subject, json, trace_headers}, state) do
    enqueue_entry({subject, json, trace_headers, nil}, state)
  end

  # Staged per subject, not per shard, because a cohort is a per-STREAM object;
  # see `flush_subject/2`. The batch-size trip is therefore per subject too: it
  # bounds the size of one atomic batch, and the broker applies that ceiling to
  # a batch, never to a shard's total backlog.
  defp enqueue_entry(entry, state) do
    subject = elem(entry, 0)
    {count, entries} = Map.get(state.queues, subject, {0, []})
    count = count + 1

    state = %{
      state
      | queues: Map.put(state.queues, subject, {count, [entry | entries]}),
        queue_count: state.queue_count + 1
    }

    if count >= state.batch_size do
      {:noreply, flush_subject(state, subject)}
    else
      {:noreply, ensure_flush_scheduled(state)}
    end
  end

  @impl true
  def handle_info({:flush_batch, token}, %{flush_token: token} = state),
    do: {:noreply, flush_queue(state)}

  def handle_info({:flush_batch, _stale_token}, state), do: {:noreply, state}

  @impl true
  def handle_info(:connect, state), do: {:noreply, ensure_subscribed(state)}

  def handle_info({:DOWN, ref, :process, _pid, _reason}, %{conn_ref: ref} = state) do
    send(self(), :connect)
    {:noreply, %{state | sid: nil, conn_ref: nil}}
  end

  def handle_info({:DOWN, _ref, :process, _pid, _reason}, state), do: {:noreply, state}

  def handle_info({:msg, %{topic: topic, body: body}}, state),
    do: {:noreply, apply_reply(topic, body, state)}

  def handle_info(:sweep, state) do
    state = drain_replies(state)
    now = Pending.now_ms()

    expired =
      Pending.expired(
        state.wire.table,
        now - state.ack_timeout_ms,
        now - state.batch_hold_ms
      )

    for row <- expired do
      row_wire(row).expire(row, state.wire)
    end

    schedule(:sweep, @sweep_interval_ms)
    {:noreply, state}
  end

  def handle_info(:gauge, state) do
    flush_counters(state.wire.counter)
    Metrics.event("Nats/PublishInflight", inflight_gauge(state))
    schedule(:gauge, @gauge_interval_ms)
    {:noreply, state}
  end

  ## Cohort assembly and wire selection

  defp ensure_flush_scheduled(%{flush_token: token} = state) when not is_nil(token), do: state

  defp ensure_flush_scheduled(state) do
    token = make_ref()
    schedule({:flush_batch, token}, state.batch_wait_ms)
    %{state | flush_token: token}
  end

  defp flush_queue(%{queue_count: 0} = state), do: %{state | flush_token: nil}

  defp flush_queue(state) do
    state = Enum.reduce(Map.keys(state.queues), state, &flush_subject(&2, &1))
    %{state | flush_token: nil}
  end

  # One cohort per subject, never one per shard.
  #
  # A cohort travels as a single ADR-050 atomic batch, and a batch belongs to
  # ONE stream: the broker resolves each message's stream from its subject and
  # keeps the batch state on that stream's mset. A batch whose messages span two
  # streams therefore arrives at each of them as a sequence that does not start
  # at 1, and the server answers that with JSAtomicPublishIncompleteBatch — on
  # messages that carry no reply inbox, so the rejection is silent and the events
  # are simply gone. Since the standard lane was partitioned onto
  # TWITCH_INGRESS_STANDARD (see pkg/bus/streams.go) that is the ordinary case,
  # not an edge one: every mixed premium/standard cohort would be lost.
  #
  # Grouped by subject rather than by stream on purpose. A subject belongs to
  # exactly one stream by construction, so subject groups can never be coarser
  # than stream groups, and ingress stays correct across any future partition
  # without carrying a copy of the broker's stream catalog that would drift from
  # it. What it costs is that premium and the stream lane no longer amortize
  # together even though they share a stream — the stream lane carries only
  # stream.online/offline, which flushes as singles either way.
  #
  # The shard's in-flight batch budget (`Ingress.Config.Publish.batch_inflight`)
  # is counted across all subjects, so splitting cohorts this way can only move
  # the fleet further under the broker's per-stream cap, never over it.
  defp flush_subject(state, subject) do
    case Map.pop(state.queues, subject) do
      {nil, _queues} -> state
      {{count, entries}, queues} -> send_cohort(state, queues, count, Enum.reverse(entries))
    end
  end

  defp send_cohort(state, queues, count, entries) do
    state = %{state | queues: queues, queue_count: state.queue_count - count}
    Pending.cohort(state.wire.counter)

    cohort_wire(state, entries).send_cohort(entries, state.wire)
    state
  end

  # A cohort rides the atomic wire only when the mode is on, it actually
  # amortizes something (two or more events), and this shard is under its
  # in-flight batch budget. That budget is a latency × flush-rate window, not a
  # mirror of the broker's per-stream cap (see `Ingress.Config.Publish`): it
  # bounds how much of this shard's traffic is riding on unresolved commits,
  # and it counts the slots the broker is still holding for swept batches.
  defp cohort_wire(%{wire_mode: :atomic} = state, [_, _ | _]) do
    if Pending.batches_inflight(state.wire.counter) < state.batch_inflight_cap do
      Atomic
    else
      # Nats/PublishBatchBypassed is a commit-latency signal: the local window
      # filled, so this cohort intentionally goes out as singles.
      Pending.batch_bypassed(state.wire.counter)
      Single
    end
  end

  defp cohort_wire(_state, _entries), do: Single

  ## PubAck reconciliation
  #
  # A reply is routed by the tag its inbox suffix carries, never by the
  # configured wire: a shard that has just switched modes, or one whose atomic
  # cohort fell back to singles, still has both row shapes outstanding.

  # Every wire write is a blocking GenServer.call, so PubAcks pile up in this
  # mailbox while the collector is inside one. The sweep must apply them before
  # it reads the clock: the monotonic clock advanced through the block too, so
  # a deadline computed first expires rows whose acknowledgements are sitting a
  # few messages further down THIS mailbox — counting stored events as publish
  # failures and deleting rows the queued replies then no-op against. One
  # stalled socket write would otherwise cost a shard its whole window
  # (publish_max_pending = 16384 events) as phantom loss.
  # Bounded by the mailbox depth measured at the tick, not by "until empty": at
  # firehose rates replies arrive while the drain runs, and an unbounded loop
  # would starve the sweep and the gauge it is standing in front of.
  defp drain_replies(state) do
    {:message_queue_len, queued} = Process.info(self(), :message_queue_len)
    drain_replies(state, queued)
  end

  defp drain_replies(state, 0), do: state

  defp drain_replies(state, remaining) do
    receive do
      {:msg, %{topic: topic, body: body}} ->
        drain_replies(apply_reply(topic, body, state), remaining - 1)
    after
      0 -> state
    end
  end

  defp apply_reply(topic, body, state) do
    case AckPath.parse(topic, state.wire.prefix) do
      nil -> state
      ref -> apply_ack(ref, body, state)
    end
  end

  defp apply_ack(ref, body, state) do
    _ = ack_wire(ref).ack(ref, body, state.wire)
    state
  end

  defp ack_wire({:single, _id}), do: Single
  defp ack_wire({:batch_start, _id}), do: Atomic
  defp ack_wire({:batch_commit, _id}), do: Atomic

  defp row_wire({_id, :single, _subject, _payload, _attempts, _stamp}), do: Single
  defp row_wire({_id, :batch, _entries, _stamp}), do: Atomic
  defp row_wire({_id, :batch_hold, _stamp}), do: Atomic

  ## Connection and metrics

  defp ensure_subscribed(state) do
    case Process.whereis(state.wire.conn) do
      nil ->
        schedule(:connect, 500)
        state

      pid ->
        subscribe(state, pid)
    end
  catch
    :exit, _ ->
      schedule(:connect, 500)
      state
  end

  # The monitor is taken only on the path that stores it. Taking it before
  # Gnat.sub/3 leaked one ref per attempt down the `catch :exit` above — the
  # branch that exists precisely because the connection died mid-call — so a
  # broker roll accumulated a stale monitor and a spurious :DOWN per retry.
  # Monitoring after the fact is not a race: Process.monitor on an
  # already-dead pid delivers :DOWN immediately, which is the reconnect path.
  defp subscribe(state, pid) do
    case Gnat.sub(state.wire.conn, self(), state.sub_topic) do
      {:ok, sid} ->
        Logger.info("nats cohort publisher #{state.index} awaiting acks on #{state.sub_topic}")

        %{state | sid: sid, conn_ref: Process.monitor(pid)}

      other ->
        Logger.warning("nats cohort publisher #{state.index} subscribe failed: #{inspect(other)}")

        schedule(:connect, 500)
        state
    end
  end

  defp inflight_gauge(state) do
    pending = Pending.pending(state.wire.counter)

    %{
      shard: state.index,
      pending: pending,
      max_pending: state.max_pending,
      utilization_pct: round(pending * 100 / state.max_pending),
      queued: state.queue_count,
      batch_size: state.batch_size,
      batches_inflight: Pending.batches_inflight(state.wire.counter)
    }
  end

  defp flush_counters(counter) do
    flush_metric(Pending.take_acked(counter), "Nats/PublishAcked")
    flush_metric(Pending.take_retried(counter), "Nats/PublishRetried")
    flush_metric(Pending.take_failed(counter), "Nats/PublishFailed")
    flush_metric(Pending.take_cohorts(counter), "Nats/PublishCohorts")
    flush_metric(Pending.take_batch_fallback(counter), "Nats/PublishBatchFallback")
    flush_metric(Pending.take_batch_bypassed(counter), "Nats/PublishBatchBypassed")

    flush_metric(
      Pending.take_batch_headers_ignored(counter),
      "Nats/PublishBatchHeadersIgnored"
    )
  end

  defp flush_metric(0, _name), do: :ok
  defp flush_metric(count, name), do: Metrics.count(name, count)

  @doc false
  @spec id_from_topic(String.t(), String.t()) :: non_neg_integer() | nil
  def id_from_topic(topic, prefix) do
    case AckPath.parse(topic, prefix) do
      {:single, id} -> id
      _ -> nil
    end
  end

  @impl true
  def terminate(_reason, state) do
    :persistent_term.erase({__MODULE__, :ctx, state.index})
    CohortSender.stop(state.wire.senders)
    :ok
  end

  defp schedule(msg, ms), do: Process.send_after(self(), msg, ms)
end
