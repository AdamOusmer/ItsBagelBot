defmodule Ingress.Nats.Publisher do
  @moduledoc """
  One scheduler-local, bounded JetStream microbatch publisher.

  Calls arriving within `publish_batch_wait_ms` are staged into one NATS atomic
  batch and receive one commit PubAck for the whole cohort. A quiet lane is sent
  individually rather than paying batch setup overhead. The caller returns once
  its message is on the wire; PubAcks are reconciled asynchronously.

  Every event retains its `Nats-Msg-Id`. If a batch is rejected (including one
  containing a duplicate ID), its final PubAck is lost, or the batch times out,
  the collector republishes every member individually under the same IDs. That
  turns an ambiguous commit into deterministic per-message PubAcks: already
  committed members ack as duplicates and missing members are stored now.

  `Ingress.Nats.PublisherPool` runs one publisher and BUS connection per online
  BEAM scheduler. Admission and batching are serialized only inside that local
  shard, with bounded fallback probing when a shard is full or disconnected.
  """

  use GenServer

  require Logger

  alias Ingress.{Config, Metrics, Nats}

  @idx_pending 1
  @idx_next_id 2
  @idx_acked 3
  @idx_retried 4
  @idx_failed 5
  @idx_batches 6
  @idx_batch_fallbacks 7

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
         %{pid: pid, conn: conn, counter: counter, max_pending: max_pending},
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
        GenServer.cast(pid, {:enqueue, subject, json, dedup_id})
        :ok
    end
  end

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

    counter = :atomics.new(7, signed: false)
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
        max_pending: max_pending
      }
    )

    state = %{
      index: index,
      conn: conn,
      table: table,
      counter: counter,
      prefix: prefix,
      sub_topic: prefix <> ">",
      sid: nil,
      conn_ref: nil,
      socket: nil,
      tls: false,
      max_pending: max_pending,
      ack_timeout_ms: Config.publish_ack_timeout_ms(),
      max_attempts: Config.publish_attempts(),
      batch_size: Config.publish_batch_size(),
      batch_wait_ms: Config.publish_batch_wait_ms(),
      write_many: Keyword.get(opts, :write_many),
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
    {:noreply, %{state | sid: nil, conn_ref: nil, socket: nil}}
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

        {batch_id, :batch, entries, timestamp} when timestamp <= deadline ->
          fallback_batch(batch_id, entries, :ack_timeout, state)

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
    flush_metric(state.counter, @idx_batches, "Nats/PublishBatches")
    flush_metric(state.counter, @idx_batch_fallbacks, "Nats/PublishBatchFallbacks")

    Metrics.event("Nats/PublishInflight", %{
      shard: state.index,
      pending: pending,
      max_pending: state.max_pending,
      utilization_pct: round(pending * 100 / state.max_pending),
      queued: state.queue_count,
      batch_size: state.batch_size
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

    case entries do
      [entry] -> send_individual_entries([entry], state)
      _ -> send_atomic_batch(entries, state)
    end
  end

  defp send_atomic_batch(entries, state) do
    batch_id = :crypto.strong_rand_bytes(12) |> Base.url_encode64(padding: false)
    count = length(entries)

    publishes =
      entries
      |> Enum.with_index(1)
      |> Enum.map(fn {{subject, json, dedup_id, _from}, sequence} ->
        headers = batch_headers(batch_id, sequence, sequence == count, dedup_id)
        reply = if sequence == count, do: batch_reply_subject(state.prefix, batch_id), else: nil
        {subject, json, reply, headers}
      end)

    result = pub_many(state, publishes)

    case result do
      :ok ->
        wire_entries =
          Enum.map(entries, fn {subject, json, dedup_id, _from} -> {subject, json, dedup_id} end)

        :ets.insert(state.table, {batch_id, :batch, wire_entries, now_ms()})
        :atomics.add(state.counter, @idx_batches, 1)
        reply_entries(entries, :ok)
        state

      {:error, _reason} ->
        # The staged prefix was never committed (or its final send was
        # ambiguous). Individual MsgId publishes reconcile it safely.
        send_individual_entries(entries, state)
    end
  end

  defp send_individual_entries(entries, state) do
    Enum.each(entries, fn {subject, json, dedup_id, from} ->
      id = :atomics.add_get(state.counter, @idx_next_id, 1)
      :ets.insert(state.table, {id, :single, subject, json, dedup_id, 1, now_ms()})

      case pub(
             state.conn,
             subject,
             json,
             single_reply_subject(state.prefix, id),
             dedup_headers(dedup_id)
           ) do
        :ok ->
          reply_entry(from, :ok)

        {:error, reason} ->
          :ets.delete(state.table, id)
          :atomics.sub(state.counter, @idx_pending, 1)
          :atomics.add(state.counter, @idx_failed, 1)
          reply_entry(from, {:error, reason})
      end
    end)

    state
  end

  defp batch_headers(batch_id, sequence, commit?, dedup_id) do
    [
      {"Nats-Batch-Id", batch_id},
      {"Nats-Batch-Sequence", Integer.to_string(sequence)},
      {"Nats-Required-Api-Level", "2"}
    ]
    |> maybe_add_header("Nats-Batch-Commit", if(commit?, do: "1", else: nil))
    |> maybe_add_header("Nats-Msg-Id", dedup_id)
  end

  defp dedup_headers(nil), do: []
  defp dedup_headers(dedup_id), do: [{"Nats-Msg-Id", dedup_id}]

  defp maybe_add_header(headers, _key, nil), do: headers
  defp maybe_add_header(headers, key, value), do: [{key, value} | headers]

  defp pub(conn, subject, json, nil, headers), do: safe_pub(conn, subject, json, headers: headers)

  defp pub(conn, subject, json, reply, headers),
    do: safe_pub(conn, subject, json, reply_to: reply, headers: headers)

  defp safe_pub(conn, subject, json, opts) do
    Gnat.pub(conn, subject, json, opts)
  catch
    :exit, _ -> {:error, :not_connected}
  end

  # Gnat owns TLS negotiation, reconnects, subscriptions and inbound parsing.
  # PublisherPool connections are otherwise dedicated to this batcher, so one
  # ordered socket send can carry the entire cohort without 128 GenServer calls.
  defp pub_many(%{write_many: writer}, publishes) when is_function(writer, 1),
    do: writer.(publishes)

  defp pub_many(%{socket: nil, conn: conn}, publishes) do
    Enum.reduce_while(publishes, :ok, fn {subject, json, reply, headers}, :ok ->
      case pub(conn, subject, json, reply, headers) do
        :ok -> {:cont, :ok}
        {:error, reason} -> {:halt, {:error, reason}}
      end
    end)
  end

  defp pub_many(state, publishes) do
    commands =
      Enum.map(publishes, fn {subject, json, reply, headers} ->
        Gnat.Command.build(:pub, subject, json, prepared_pub_opts(reply, headers))
      end)

    if state.tls do
      :ssl.send(state.socket, commands)
    else
      :gen_tcp.send(state.socket, commands)
    end
  catch
    :exit, _ -> {:error, :not_connected}
  end

  defp prepared_pub_opts(nil, headers), do: [headers: :cow_http.headers(headers)]

  defp prepared_pub_opts(reply, headers),
    do: [headers: :cow_http.headers(headers), reply_to: reply]

  defp reply_entries(entries, result) do
    Enum.each(entries, fn {_subject, _json, _dedup_id, from} -> reply_entry(from, result) end)
  end

  defp reply_entry(nil, _result), do: :ok
  defp reply_entry(from, result), do: GenServer.reply(from, result)

  ## PubAck reconciliation

  defp apply_ack({:batch, batch_id}, body, state) do
    case :ets.lookup(state.table, batch_id) do
      [{^batch_id, :batch, entries, _timestamp}] ->
        case Nats.parse_pub_ack(body) do
          :ok -> resolve_batch(batch_id, length(entries), state)
          {:error, reason} -> fallback_batch(batch_id, entries, reason, state)
        end

      [] ->
        state
    end
  end

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

  defp resolve_batch(batch_id, count, state) do
    :ets.delete(state.table, batch_id)
    :atomics.sub(state.counter, @idx_pending, count)
    :atomics.add(state.counter, @idx_acked, count)
    state
  end

  defp resolve_single(id, state) do
    :ets.delete(state.table, id)
    :atomics.sub(state.counter, @idx_pending, 1)
    :atomics.add(state.counter, @idx_acked, 1)
    state
  end

  defp fallback_batch(batch_id, entries, _reason, state) do
    case :ets.take(state.table, batch_id) do
      [] ->
        state

      [_row] ->
        :atomics.add(state.counter, @idx_batch_fallbacks, 1)
        :atomics.add(state.counter, @idx_retried, length(entries))

        entries
        |> Enum.map(fn {subject, json, dedup_id} -> {subject, json, dedup_id, nil} end)
        |> send_individual_entries(state)
    end
  end

  defp retry_or_drop(id, subject, json, dedup_id, attempts, _reason, state) do
    if attempts < state.max_attempts do
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

  defp ack_key(topic, prefix) do
    plen = byte_size(prefix)

    case topic do
      <<^prefix::binary-size(plen), "b.", batch_id::binary>> when batch_id != "" ->
        {:batch, batch_id}

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

  defp parse_single_id(id) do
    case Integer.parse(id) do
      {value, ""} -> {:single, value}
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
  defp batch_reply_subject(prefix, batch_id), do: prefix <> "b." <> batch_id

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
            {socket, tls?} = connection_writer(pid)
            Logger.info("nats batch publisher #{state.index} awaiting acks on #{state.sub_topic}")
            %{state | sid: sid, conn_ref: ref, socket: socket, tls: tls?}

          other ->
            Process.demonitor(ref, [:flush])

            Logger.warning(
              "nats batch publisher #{state.index} subscribe failed: #{inspect(other)}"
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

  defp connection_writer(pid) do
    case :sys.get_state(pid) do
      %{socket: socket, connection_settings: %{tls: true}} -> {socket, true}
      %{socket: socket} -> {socket, false}
      _ -> {nil, false}
    end
  catch
    :exit, _ -> {nil, false}
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
    :ok
  end

  defp schedule(msg, ms), do: Process.send_after(self(), msg, ms)
end
