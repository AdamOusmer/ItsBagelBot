defmodule Ingress.Nats.Publisher do
  @moduledoc """
  One shard of the asynchronous, pipelined JetStream publisher for the lane
  firehose. `Ingress.Nats.PublisherPool` runs `publish_connections` of these,
  each bound to its own BUS connection.

  ## Why async, and why sharded

  The synchronous `Gnat.request/4` path capped in-flight publishes at the
  dispatcher's worker-pool size: each worker blocked on one broker PubAck, so
  peak throughput was `pool_size / ack_latency` — at ~100 ms PubAck only a few
  thousand events/second, no matter how many shards or pods, because every
  connection converged on the same shared JetStream ceiling.

  Async publishing decouples in-flight count from the worker pool: each publish
  is a core NATS `pub` carrying a private reply subject and a `Nats-Msg-Id`
  dedup header, and the broker's PubAck lands asynchronously on that reply
  subject. `max_pending` publishes ride the wire at once, so ack latency no
  longer throttles throughput — it only sets how many acks are outstanding
  (`max_pending / ack_latency` events/second).

  But a single async publisher still funnels every `Gnat.pub` through one Gnat
  connection process, and every PubAck through one collector process — one BEAM
  process tops out well below a 150-200k/s target. So the pool shards the load
  across N independent connections, each with its own collector, pending table
  and inbox namespace. `enqueue/3` picks the caller's current scheduler shard
  without a shared round-robin hotspot. Production runs one connection per
  online BEAM scheduler, so every configured connection carries the same direct
  path.

  ## Backpressure and reliability

  Backpressure is an explicit per-shard bound: once `max_pending` publishes are
  outstanding on a shard, further publishes routed there are refused
  (`{:error, :overloaded}`) and the caller drops, so memory stays bounded under
  a broker stall instead of buffering without limit. The `Nats-Msg-Id` still
  collapses retries and Twitch redeliveries at the broker, so a PubAck that
  errors or never arrives within `ack_timeout_ms` is safely re-published (up to
  `publish_attempts`) before the event is dropped with a metric.

  ## Per-shard shape

    * a public `:set` ETS table, one row per outstanding publish, owned
      by the collector so it is torn down if the collector dies.
    * an `:atomics` array holding the outstanding count, monotonic id allocator
      and batched metric counters, so neither publishes nor PubAcks need a
      telemetry-process round-trip per event.
    * a `:persistent_term` context (`{__MODULE__, :ctx, index}`) so `enqueue/3`,
      called from many workers concurrently, reads the counter, prefix, table
      and connection without copying.
    * the collector `GenServer` owns the reply-subject subscription, applies
      acks, and runs the timeout sweep.
  """

  use GenServer

  require Logger

  alias Ingress.{Config, Metrics, Nats}

  # `:atomics` indexes: outstanding publishes, the monotonic reply-id allocator,
  # and counters flushed to New Relic on the periodic gauge tick. Sending one
  # New Relic cast per PubAck makes telemetry itself a firehose at production
  # rates; atomics preserve the exact totals with constant mailbox traffic.
  @idx_pending 1
  @idx_next_id 2
  @idx_acked 3
  @idx_retried 4
  @idx_failed 5

  # Reply subjects live under `_INBOX.>`, which the BUS account already permits
  # (the old `Gnat.request` path used the same namespace); the random token
  # keeps one shard's acks from landing on another shard's or pod's collector.
  @inbox_prefix "_INBOX.ingresspub."

  @sweep_interval_ms 500
  @gauge_interval_ms 5_000

  ## Hot path (runs in the caller — dispatcher workers, squash tasks)

  @doc """
  Admit one publish into a shard's in-flight window, or refuse it.

  The shard is chosen by scheduler id, keeping the normal path on a local
  connection without a shared counter or per-message hash. Only saturation or
  a disconnected shard triggers a bounded probe for spare sibling capacity.
  `publish_connections` normally equals the online scheduler count.

  Returns `:ok` once the event is on the wire (its PubAck is now awaited
  asynchronously), `{:error, :overloaded}` when its shard is full, or
  `{:error, :not_connected}` when its connection cannot publish.
  """
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

  # The scheduler-local shard is always attempted first. Only an unavailable or
  # saturated shard probes siblings, so the normal path remains contention-free
  # while spare connections absorb imbalance instead of producing false drops.
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

  defp admit(%{counter: counter, max_pending: max_pending} = ctx, subject, json, dedup_id) do
    if :atomics.add_get(counter, @idx_pending, 1) > max_pending do
      :atomics.sub(counter, @idx_pending, 1)
      {:error, :overloaded}
    else
      do_publish(ctx, subject, json, dedup_id)
    end
  end

  defp do_publish(
         %{counter: counter, prefix: prefix, table: table, conn: conn},
         subject,
         json,
         dedup_id
       ) do
    id = :atomics.add_get(counter, @idx_next_id, 1)
    now = System.monotonic_time(:millisecond)
    :ets.insert(table, {id, subject, json, dedup_id, 1, now})

    case pub(conn, subject, json, reply_subject(prefix, id), dedup_id) do
      :ok ->
        :ok

      {:error, reason} ->
        :ets.delete(table, id)
        :atomics.sub(counter, @idx_pending, 1)
        {:error, reason}
    end
  rescue
    # The collector died and took its ETS table with it (or a stale context is
    # briefly live during its restart). Treat as a transient outage and undo the
    # admission on the same counter the hot path incremented.
    ArgumentError ->
      :atomics.sub(counter, @idx_pending, 1)
      {:error, :not_connected}
  end

  defp pub(conn, subject, json, reply, dedup_id) do
    Gnat.pub(conn, subject, json, [reply_to: reply] ++ dedup_headers(dedup_id))
  catch
    # The BUS connection process is gone; its supervisor reconnects on its own
    # backoff and the sweep will retry this id.
    :exit, _ -> {:error, :not_connected}
  end

  defp dedup_headers(nil), do: []
  defp dedup_headers(dedup_id), do: [headers: [{"Nats-Msg-Id", dedup_id}]]

  defp reply_subject(prefix, id), do: prefix <> Integer.to_string(id)

  @doc false
  # Parse the reply id back out of a PubAck's topic, or nil if it does not
  # belong to this collector's inbox namespace.
  @spec id_from_topic(String.t(), String.t()) :: non_neg_integer() | nil
  def id_from_topic(topic, prefix) do
    plen = byte_size(prefix)

    case topic do
      <<^prefix::binary-size(plen), rest::binary>> ->
        case Integer.parse(rest) do
          {id, ""} -> id
          _ -> nil
        end

      _ ->
        nil
    end
  end

  ## Collector

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

    counter = :atomics.new(5, signed: false)
    token = :crypto.strong_rand_bytes(9) |> Base.url_encode64(padding: false)
    prefix = @inbox_prefix <> token <> "."
    max_pending = Config.publish_max_pending()

    :persistent_term.put(
      {__MODULE__, :ctx, index},
      %{counter: counter, prefix: prefix, table: table, conn: conn, max_pending: max_pending}
    )

    state = %{
      index: index,
      conn: conn,
      table: table,
      counter: counter,
      prefix: prefix,
      sub_topic: prefix <> "*",
      sid: nil,
      conn_ref: nil,
      max_pending: max_pending,
      ack_timeout_ms: Config.publish_ack_timeout_ms(),
      max_attempts: Config.publish_attempts()
    }

    send(self(), :connect)
    schedule(:sweep, @sweep_interval_ms)
    schedule(:gauge, @gauge_interval_ms)
    {:ok, state}
  end

  @impl true
  def handle_info(:connect, state), do: {:noreply, ensure_subscribed(state)}

  # The BUS connection died: our reply-subject subscription went with it. Drop
  # the stale sid and resubscribe once the connection supervisor reconnects.
  def handle_info({:DOWN, ref, :process, _pid, _reason}, %{conn_ref: ref} = state) do
    send(self(), :connect)
    {:noreply, %{state | sid: nil, conn_ref: nil}}
  end

  def handle_info({:DOWN, _ref, :process, _pid, _reason}, state), do: {:noreply, state}

  # A PubAck (or JetStream error) for one outstanding publish.
  def handle_info({:msg, %{topic: topic, body: body}}, state) do
    case id_from_topic(topic, state.prefix) do
      nil -> {:noreply, state}
      id -> {:noreply, apply_ack(id, body, state)}
    end
  end

  def handle_info(:sweep, state) do
    deadline = System.monotonic_time(:millisecond) - state.ack_timeout_ms

    match = [
      {{:"$1", :"$2", :"$3", :"$4", :"$5", :"$6"}, [{:"=<", :"$6", deadline}],
       [{{:"$1", :"$2", :"$3", :"$4", :"$5"}}]}
    ]

    for {id, subject, json, dedup_id, attempts} <- :ets.select(state.table, match) do
      retry_or_drop(id, subject, json, dedup_id, attempts, :ack_timeout, state)
    end

    schedule(:sweep, @sweep_interval_ms)
    {:noreply, state}
  end

  def handle_info(:gauge, state) do
    pending = :atomics.get(state.counter, @idx_pending)

    flush_metric(state.counter, @idx_acked, "Nats/PublishAcked")
    flush_metric(state.counter, @idx_retried, "Nats/PublishRetried")
    flush_metric(state.counter, @idx_failed, "Nats/PublishFailed")

    Metrics.event("Nats/PublishInflight", %{
      shard: state.index,
      pending: pending,
      max_pending: state.max_pending,
      utilization_pct: round(pending * 100 / state.max_pending)
    })

    schedule(:gauge, @gauge_interval_ms)
    {:noreply, state}
  end

  defp ensure_subscribed(state) do
    case Process.whereis(state.conn) do
      nil ->
        schedule(:connect, 500)
        state

      pid ->
        ref = Process.monitor(pid)

        case Gnat.sub(state.conn, self(), state.sub_topic) do
          {:ok, sid} ->
            Logger.info("nats async publisher #{state.index} awaiting acks on #{state.sub_topic}")
            %{state | sid: sid, conn_ref: ref}

          other ->
            Process.demonitor(ref, [:flush])

            Logger.warning(
              "nats async publisher #{state.index} subscribe failed: #{inspect(other)}"
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

  defp apply_ack(id, body, state) do
    case :ets.lookup(state.table, id) do
      # Already resolved — a timeout retry raced this ack, or it is a stray.
      [] ->
        state

      [{^id, subject, json, dedup_id, attempts, _ts}] ->
        case Nats.parse_pub_ack(body) do
          :ok ->
            resolve(id, state)

          {:error, reason} ->
            retry_or_drop(id, subject, json, dedup_id, attempts, reason, state)
        end

        state
    end
  end

  defp resolve(id, state) do
    :ets.delete(state.table, id)
    :atomics.sub(state.counter, @idx_pending, 1)
    :atomics.add(state.counter, @idx_acked, 1)
  end

  # Re-publish under the same reply id and `Nats-Msg-Id` (dedup makes the retry
  # a no-op at the broker if the first copy did land) until the attempt budget
  # is spent, then drop with a metric so the outstanding count stays honest.
  defp retry_or_drop(id, subject, json, dedup_id, attempts, _reason, state) do
    if attempts < state.max_attempts do
      :ets.insert(state.table, {id, subject, json, dedup_id, attempts + 1, now_ms()})
      :atomics.add(state.counter, @idx_retried, 1)
      _ = pub(state.conn, subject, json, reply_subject(state.prefix, id), dedup_id)
      :ok
    else
      :ets.delete(state.table, id)
      :atomics.sub(state.counter, @idx_pending, 1)
      :atomics.add(state.counter, @idx_failed, 1)

      :ok
    end
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
