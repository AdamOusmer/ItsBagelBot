defmodule Ingress.Nats.Publisher do
  @moduledoc """
  One scheduler-local, bounded JetStream cohort publisher.

  Calls arriving within `publish_batch_wait_ms` are staged as a local cohort,
  then published through Gnat on one of two wires (`Config.publish_wire/0`):

    * `:single` (default) — one ordinary JetStream PubAck per event. With
      `Config.publish_dedup/0` on, each event retains its `Nats-Msg-Id`, so a
      missing PubAck is retried safely and the broker folds the replay. With
      dedup off (the production setting — the dedup insert costs ~27% of
      single-stream ingest capacity and EventSub websockets never redeliver),
      ids are stripped at admission and an ambiguous ack timeout drops the
      event instead of retrying; only definite failures retry.
    * `:atomic` — the cohort is written as one ADR-050 atomic batch (NATS
      2.14): sequenced `Nats-Batch-*` headers, one commit PubAck for the whole
      cohort. Events keep their `Nats-Msg-Id` (deduplication is enforced inside
      batches since 2.12.1). A rejected, abandoned or unacknowledged batch is
      re-driven per message over the `:single` machinery: atomicity means the
      broker stored nothing (the retry stores everything exactly once) or
      everything (the retry folds every duplicate), so the fallback can never
      double-store. Fast-Ingest (flow-controlled batches) stays out of scope.

  `Ingress.Nats.PublisherPool` runs one publisher and BUS connection per online
  BEAM scheduler. Admission and cohort assembly are serialized only inside that local
  shard, with bounded fallback probing when a shard is full or disconnected.
  """

  use GenServer

  require Logger

  alias Ingress.{Config, Metrics, Nats}
  alias Ingress.Nats.CohortSender

  @idx_pending 1
  @idx_next_id 2
  @idx_acked 3
  @idx_retried 4
  @idx_failed 5
  @idx_cohorts 6
  @idx_batch_inflight 7
  @idx_batch_fallback 8

  @inbox_prefix "_INBOX.ingresspub."
  @sweep_interval_ms 500
  @gauge_interval_ms 5_000

  ## Scheduler-local admission

  @spec enqueue(String.t(), iodata(), String.t() | nil) :: :ok | {:error, term()}
  def enqueue(subject, json, dedup_id) do
    case :persistent_term.get({__MODULE__, :n}, 0) do
      0 ->
        {:error, :not_connected}

      n ->
        index = rem(max(:erlang.system_info(:scheduler_id), 1) - 1, n)
        enqueue_from(index, n, subject, json, dedup_id, nil)
    end
  end

  defp enqueue_from(_index, 0, _subject, _json, _dedup_id, nil),
    do: {:error, :not_connected}

  defp enqueue_from(_index, 0, _subject, _json, _dedup_id, last_error), do: last_error

  defp enqueue_from(index, remaining, subject, json, dedup_id, _last_error) do
    n = :persistent_term.get({__MODULE__, :n})

    result =
      case :persistent_term.get({__MODULE__, :ctx, index}, nil) do
        nil -> {:error, :not_connected}
        ctx -> admit(ctx, subject, json, dedup_id)
      end

    case result do
      :ok ->
        :ok

      {:error, reason} = error when reason in [:overloaded, :not_connected] ->
        enqueue_from(rem(index + 1, n), remaining - 1, subject, json, dedup_id, error)

      error ->
        error
    end
  end

  defp admit(
         %{pid: pid, conn: conn, counter: counter, max_pending: max_pending} = ctx,
         subject,
         json,
         dedup_id
       ) do
    cond do
      :atomics.add_get(counter, @idx_pending, 1) > max_pending ->
        :atomics.sub(counter, @idx_pending, 1)
        {:error, :overloaded}

      not Process.alive?(pid) or is_nil(Process.whereis(conn)) ->
        :atomics.sub(counter, @idx_pending, 1)
        {:error, :not_connected}

      true ->
        GenServer.cast(pid, {:enqueue, subject, json, admitted_dedup_id(ctx, dedup_id)})
        :ok
    end
  end

  # With dedup disabled the id is stripped at admission, so everything
  # downstream — wire headers, retry policy, batch fallback — sees the event
  # as unprotected and behaves at-most-once on ambiguity.
  defp admitted_dedup_id(%{dedup: false}, _dedup_id), do: nil
  defp admitted_dedup_id(_ctx, dedup_id), do: dedup_id

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
    conn = Keyword.fetch!(opts, :conn)
    table = :"ingress_pub_pending_#{index}"

    :ets.new(table, [
      :named_table,
      :public,
      :set,
      read_concurrency: true,
      write_concurrency: true
    ])

    counter = :atomics.new(8, signed: false)
    token = :crypto.strong_rand_bytes(9) |> Base.url_encode64(padding: false)
    prefix = @inbox_prefix <> token <> "."
    max_pending = Config.publish_max_pending()

    :persistent_term.put(
      {__MODULE__, :ctx, index},
      %{
        pid: self(),
        counter: counter,
        prefix: prefix,
        table: table,
        conn: conn,
        max_pending: max_pending,
        dedup: Config.publish_dedup()
      }
    )

    state = %{
      index: index,
      conn: conn,
      table: table,
      counter: counter,
      prefix: prefix,
      batch_token: token,
      sub_topic: prefix <> ">",
      sid: nil,
      conn_ref: nil,
      max_pending: max_pending,
      ack_timeout_ms: Config.publish_ack_timeout_ms(),
      max_attempts: Config.publish_attempts(),
      batch_size: Config.publish_batch_size(),
      batch_wait_ms: Config.publish_batch_wait_ms(),
      senders: CohortSender.start(Config.publish_send_concurrency()),
      wire: Config.publish_wire(),
      batch_inflight_cap: Config.publish_batch_inflight(),
      queue: [],
      queue_count: 0,
      flush_token: nil
    }

    send(self(), :connect)
    schedule(:sweep, @sweep_interval_ms)
    schedule(:gauge, @gauge_interval_ms)
    {:ok, state}
  end

  @impl true
  def handle_cast({:enqueue, subject, json, dedup_id}, state) do
    state = %{
      state
      | queue: [{subject, json, dedup_id, nil} | state.queue],
        queue_count: state.queue_count + 1
    }

    if state.queue_count >= state.batch_size do
      {:noreply, flush_queue(state)}
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

  def handle_info({:msg, %{topic: topic, body: body}}, state) do
    case ack_key(topic, state.prefix) do
      nil -> {:noreply, state}
      key -> {:noreply, apply_ack(key, body, state)}
    end
  end

  def handle_info(:sweep, state) do
    deadline = now_ms() - state.ack_timeout_ms

    for row <- :ets.tab2list(state.table) do
      case row do
        {id, :single, subject, json, dedup_id, attempts, timestamp} when timestamp <= deadline ->
          retry_or_drop(id, subject, json, dedup_id, attempts, :ack_timeout, state)

        {id, :batch, entries, timestamp} when timestamp <= deadline ->
          expire_batch(id, entries, state)

        _ ->
          :ok
      end
    end

    schedule(:sweep, @sweep_interval_ms)
    {:noreply, state}
  end

  def handle_info(:gauge, state) do
    pending = :atomics.get(state.counter, @idx_pending)

    flush_metric(state.counter, @idx_acked, "Nats/PublishAcked")
    flush_metric(state.counter, @idx_retried, "Nats/PublishRetried")
    flush_metric(state.counter, @idx_failed, "Nats/PublishFailed")
    flush_metric(state.counter, @idx_cohorts, "Nats/PublishCohorts")
    flush_metric(state.counter, @idx_batch_fallback, "Nats/PublishBatchFallback")

    Metrics.event("Nats/PublishInflight", %{
      shard: state.index,
      pending: pending,
      max_pending: state.max_pending,
      utilization_pct: round(pending * 100 / state.max_pending),
      queued: state.queue_count,
      batch_size: state.batch_size,
      batches_inflight: :atomics.get(state.counter, @idx_batch_inflight)
    })

    schedule(:gauge, @gauge_interval_ms)
    {:noreply, state}
  end

  ## Batch assembly and wire writes

  defp ensure_flush_scheduled(%{flush_token: token} = state) when not is_nil(token), do: state

  defp ensure_flush_scheduled(state) do
    token = make_ref()
    schedule({:flush_batch, token}, state.batch_wait_ms)
    %{state | flush_token: token}
  end

  defp flush_queue(%{queue_count: 0} = state), do: %{state | flush_token: nil}

  defp flush_queue(state) do
    entries = Enum.reverse(state.queue)
    state = %{state | queue: [], queue_count: 0, flush_token: nil}
    :atomics.add(state.counter, @idx_cohorts, 1)

    if atomic_batch?(state, entries) do
      send_atomic_batch(entries, state)
    else
      send_individual_entries(entries, state)
    end
  end

  # A cohort rides the atomic wire only when the mode is on, it actually
  # amortizes something (two or more events), and this shard is under its
  # in-flight batch budget — the broker caps in-flight batches per stream, so
  # overflow degrades to per-message publishes instead of broker rejections.
  defp atomic_batch?(%{wire: :atomic} = state, [_, _ | _]),
    do: :atomics.get(state.counter, @idx_batch_inflight) < state.batch_inflight_cap

  defp atomic_batch?(_state, _entries), do: false

  defp send_individual_entries(entries, state) do
    requests = Enum.map(entries, &stage_single(&1, state))

    state.senders
    |> CohortSender.publish(state.conn, requests)
    |> Enum.each(&finish_single_send(&1, state))

    state
  end

  defp stage_single({subject, json, dedup_id, from}, state) do
    id = :atomics.add_get(state.counter, @idx_next_id, 1)
    :ets.insert(state.table, {id, :single, subject, json, dedup_id, 1, now_ms()})

    token = {id, from}
    opts = [reply_to: single_reply_subject(state.prefix, id), headers: dedup_headers(dedup_id)]
    {token, subject, json, opts}
  end

  defp finish_single_send({{_id, from}, :ok}, _state), do: reply_entry(from, :ok)

  defp finish_single_send({{id, from}, {:error, reason}}, state) do
    :ets.delete(state.table, id)
    :atomics.sub(state.counter, @idx_pending, 1)
    :atomics.add(state.counter, @idx_failed, 1)
    reply_entry(from, {:error, reason})
  end

  ## Atomic batch wire (ADR-050)

  # Publishes one cohort as an atomic batch: sequenced Nats-Batch-* headers,
  # the opening message carrying a reply (so a rejected open surfaces at once),
  # intermediates unacknowledged, and the final message committing the batch
  # into one PubAck. The whole cohort is tracked as a single ETS row until that
  # commit ack, an error reply, or the sweep deadline resolves it.
  defp send_atomic_batch(entries, state) do
    id = :atomics.add_get(state.counter, @idx_next_id, 1)
    batch_id = state.batch_token <> "-" <> Integer.to_string(id)
    :ets.insert(state.table, {id, :batch, entries, now_ms()})
    :atomics.add(state.counter, @idx_batch_inflight, 1)

    case publish_batch_messages(entries, batch_id, id, state) do
      :ok -> state
      {:error, _reason} -> fallback_batch(id, entries, state)
    end
  end

  defp publish_batch_messages(entries, batch_id, id, state) do
    last = length(entries)

    entries
    |> Enum.with_index(1)
    |> Enum.reduce_while(:ok, fn {{subject, json, dedup_id, _from}, seq}, :ok ->
      headers = batch_headers(batch_id, seq, last, dedup_id)

      case safe_pub(state.conn, subject, json, batch_pub_opts(headers, seq, last, id, state)) do
        :ok -> {:cont, :ok}
        {:error, reason} -> {:halt, {:error, reason}}
      end
    end)
  end

  defp batch_headers(batch_id, seq, last, dedup_id) do
    commit = if seq == last, do: [{"Nats-Batch-Commit", "1"}], else: []

    [{"Nats-Batch-Id", batch_id}, {"Nats-Batch-Sequence", Integer.to_string(seq)}] ++
      commit ++ dedup_headers(dedup_id)
  end

  defp batch_pub_opts(headers, 1, last, id, state) when last > 1,
    do: [reply_to: state.prefix <> "bs." <> Integer.to_string(id), headers: headers]

  defp batch_pub_opts(headers, seq, last, id, state) when seq == last,
    do: [reply_to: state.prefix <> "bc." <> Integer.to_string(id), headers: headers]

  defp batch_pub_opts(headers, _seq, _last, _id, _state), do: [headers: headers]

  # Re-drives a failed batch per message over the single wire. Atomicity plus
  # per-message Nats-Msg-Id makes this converge from either half-state: an
  # abandoned batch stored nothing, a committed batch whose ack was lost folds
  # every re-publish as a duplicate.
  defp fallback_batch(id, entries, state) do
    :ets.delete(state.table, id)
    :atomics.sub(state.counter, @idx_batch_inflight, 1)
    :atomics.add(state.counter, @idx_batch_fallback, 1)
    send_individual_entries(entries, state)
  end

  # A swept batch is the cohort-shaped ack timeout: the commit may have landed
  # with only its ack lost. Protected entries re-drive and the broker folds
  # duplicates; unprotected entries (dedup off) are dropped whole rather than
  # risking a double-stored cohort. Error replies never come here — they are
  # definite rejections and take fallback_batch directly.
  defp expire_batch(id, [{_subject, _json, nil, _from} | _] = entries, state) do
    :ets.delete(state.table, id)
    count = length(entries)
    :atomics.sub(state.counter, @idx_pending, count)
    :atomics.add(state.counter, @idx_failed, count)
    :atomics.sub(state.counter, @idx_batch_inflight, 1)
    state
  end

  defp expire_batch(id, entries, state), do: fallback_batch(id, entries, state)

  defp resolve_batch(id, entries, state) do
    :ets.delete(state.table, id)
    count = length(entries)
    :atomics.sub(state.counter, @idx_pending, count)
    :atomics.add(state.counter, @idx_acked, count)
    :atomics.sub(state.counter, @idx_batch_inflight, 1)
    state
  end

  defp dedup_headers(nil), do: []
  defp dedup_headers(dedup_id), do: [{"Nats-Msg-Id", dedup_id}]

  defp pub(conn, subject, json, reply, headers),
    do: safe_pub(conn, subject, json, reply_to: reply, headers: headers)

  defp safe_pub(conn, subject, json, opts) do
    Gnat.pub(conn, subject, json, opts)
  catch
    :exit, _ -> {:error, :not_connected}
  end

  defp reply_entry(nil, _result), do: :ok
  defp reply_entry(from, result), do: GenServer.reply(from, result)

  ## PubAck reconciliation

  defp apply_ack({:single, id}, body, state) do
    case :ets.lookup(state.table, id) do
      [{^id, :single, subject, json, dedup_id, attempts, _timestamp}] ->
        case Nats.parse_pub_ack(body) do
          :ok -> resolve_single(id, state)
          {:error, reason} -> retry_or_drop(id, subject, json, dedup_id, attempts, reason, state)
        end

      [] ->
        state
    end
  end

  # Batch-open reply: zero-byte means the broker accepted the batch and the
  # commit ack will resolve it. A non-empty body is an immediate rejection
  # (unsupported stream, in-flight limit, duplicate id) — fall back now instead
  # of waiting out the sweep deadline.
  defp apply_ack({:batch_start, _id}, "", state), do: state

  defp apply_ack({:batch_start, id}, _body, state) do
    case :ets.lookup(state.table, id) do
      [{^id, :batch, entries, _timestamp}] -> fallback_batch(id, entries, state)
      _ -> state
    end
  end

  defp apply_ack({:batch_commit, id}, body, state) do
    case :ets.lookup(state.table, id) do
      [{^id, :batch, entries, _timestamp}] ->
        case Nats.parse_pub_ack(body) do
          :ok -> resolve_batch(id, entries, state)
          {:error, _reason} -> fallback_batch(id, entries, state)
        end

      _ ->
        state
    end
  end

  defp resolve_single(id, state) do
    :ets.delete(state.table, id)
    :atomics.sub(state.counter, @idx_pending, 1)
    :atomics.add(state.counter, @idx_acked, 1)
    state
  end

  defp retry_or_drop(id, subject, json, dedup_id, attempts, reason, state) do
    if retry?(dedup_id, attempts, reason, state) do
      :ets.insert(
        state.table,
        {id, :single, subject, json, dedup_id, attempts + 1, now_ms()}
      )

      :atomics.add(state.counter, @idx_retried, 1)

      _ =
        pub(
          state.conn,
          subject,
          json,
          single_reply_subject(state.prefix, id),
          dedup_headers(dedup_id)
        )

      :ok
    else
      :ets.delete(state.table, id)
      :atomics.sub(state.counter, @idx_pending, 1)
      :atomics.add(state.counter, @idx_failed, 1)
      :ok
    end
  end

  # An ack timeout is ambiguous: the broker may have stored the event and only
  # the ack was lost. Without a Nats-Msg-Id nothing folds a re-publish, so an
  # unprotected event is dropped rather than risking a duplicate (at-most-once
  # on ambiguity). Definite failures — error PubAcks, socket errors — mean
  # nothing was stored, so they stay retried with or without dedup.
  defp retry?(nil, _attempts, :ack_timeout, _state), do: false
  defp retry?(_dedup_id, attempts, _reason, state), do: attempts < state.max_attempts

  defp ack_key(topic, prefix) do
    plen = byte_size(prefix)

    case topic do
      <<^prefix::binary-size(plen), "bs.", id::binary>> ->
        parse_tagged_id(:batch_start, id)

      <<^prefix::binary-size(plen), "bc.", id::binary>> ->
        parse_tagged_id(:batch_commit, id)

      <<^prefix::binary-size(plen), "s.", id::binary>> ->
        parse_single_id(id)

      <<^prefix::binary-size(plen), id::binary>> ->
        # Backwards-compatible parser for in-flight replies across a rolling
        # upgrade and for the public id_from_topic/2 contract.
        parse_single_id(id)

      _ ->
        nil
    end
  end

  defp parse_single_id(id), do: parse_tagged_id(:single, id)

  defp parse_tagged_id(tag, id) do
    case Integer.parse(id) do
      {value, ""} -> {tag, value}
      _ -> nil
    end
  end

  @doc false
  @spec id_from_topic(String.t(), String.t()) :: non_neg_integer() | nil
  def id_from_topic(topic, prefix) do
    case ack_key(topic, prefix) do
      {:single, id} -> id
      _ -> nil
    end
  end

  defp single_reply_subject(prefix, id), do: prefix <> "s." <> Integer.to_string(id)
  ## Connection and metrics

  defp ensure_subscribed(state) do
    case Process.whereis(state.conn) do
      nil ->
        schedule(:connect, 500)
        state

      pid ->
        ref = Process.monitor(pid)

        case Gnat.sub(state.conn, self(), state.sub_topic) do
          {:ok, sid} ->
            Logger.info(
              "nats cohort publisher #{state.index} awaiting acks on #{state.sub_topic}"
            )

            %{state | sid: sid, conn_ref: ref}

          other ->
            Process.demonitor(ref, [:flush])

            Logger.warning(
              "nats cohort publisher #{state.index} subscribe failed: #{inspect(other)}"
            )

            schedule(:connect, 500)
            state
        end
    end
  catch
    :exit, _ ->
      schedule(:connect, 500)
      state
  end

  defp now_ms, do: System.monotonic_time(:millisecond)

  defp flush_metric(counter, index, name) do
    case :atomics.exchange(counter, index, 0) do
      0 -> :ok
      count -> Metrics.count(name, count)
    end
  end

  @impl true
  def terminate(_reason, state) do
    :persistent_term.erase({__MODULE__, :ctx, state.index})
    CohortSender.stop(state.senders)
    :ok
  end

  defp schedule(msg, ms), do: Process.send_after(self(), msg, ms)
end
