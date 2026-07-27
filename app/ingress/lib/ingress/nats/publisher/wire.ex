defmodule Ingress.Nats.Publisher.Wire do
  @moduledoc """
  How one flushed cohort reaches JetStream, and how its acknowledgement is read
  back.

  A wire is a set of pure-ish functions over the immutable shard context in
  this module's struct. The collector GenServer is the only process that calls
  them: they touch the shard's ETS table, its `:atomics` counter and its Gnat
  connection, and hand back `:ok`. No wire owns state of its own, so the
  collector's process model — one shard, one connection, one ack inbox — is
  unchanged whichever wire is configured.

  Implementations:

    * `Ingress.Nats.Publisher.Wire.Single` — one ordinary JetStream PubAck per
      event. Definite rejections retry inside the attempt budget; anything
      ambiguous drops.
    * `Ingress.Nats.Publisher.Wire.Atomic` — the cohort as one ADR-050 atomic
      batch (NATS 2.14): sequenced `Nats-Batch-*` headers and one commit PubAck
      for the whole cohort, so an R3 stream pays quorum latency once per cohort
      instead of once per event. Falls back to `Single` when the broker
      definitely rejects the batch.
    * `Wire.FastIngest` — **not implemented, seam only.** NATS 2.14
      flow-controlled batches: a header-plus-reply-inbox protocol shaped like
      `Atomic`, but the broker flow-acks every N messages instead of only at
      the end, and an explicit end-of-batch message commits. It needs client
      support Gnat does not have (the flow-ack window has to be tracked and
      respected mid-batch), so it stays deferred; when Gnat gains it, it lands
      as a third module behind these same three callbacks plus its own
      `AckPath` tag — and, if it ever populates the reserved `from` slot, under
      the reply-exactly-once rule spelled out on `t:entry/0`.

  ## Dedup is structurally absent

  No wire attaches `Nats-Msg-Id`. EventSub websockets do not replay, and the
  broker's dedup index materially reduces ingest throughput. The consequence is
  the rule every implementation follows: an *ambiguous* outcome (ack timeout,
  malformed acknowledgement, a send error after part of a batch is already on
  the socket) drops, because a replay could double-store. Only a *definite*
  negative PubAck — which proves the broker stored nothing — may be re-driven.
  """

  alias Ingress.{Metrics, Nats}
  alias Ingress.Nats.Publisher.Pending

  @ack_latency_sample_rate 64

  @typedoc """
  One queued event. The narrow tuple is the unsampled firehose shape; the wider
  one carries trace headers for the sparse sampled events.

  `from` is **reserved and always `nil` today**: every shipped enqueue path is
  a `GenServer.cast`, so no caller is ever waiting on a wire. The slot is kept
  because a wire that acquires a synchronous caller must not change the entry
  shape underneath the collector. The rule any future wire (`Wire.FastIngest`)
  has to follow if it starts populating it: `reply/2` exactly once per entry,
  on *every* terminal path — send error, definite rejection after the attempt
  budget, sweep expiry, and success — or a populated `from` deadlocks its
  caller for that caller's full call timeout. `reply/2` already no-ops on
  `nil`, so the discipline costs nothing while the slot stays reserved.
  """
  @type entry ::
          {String.t(), iodata(), GenServer.from() | nil}
          | {String.t(), iodata(), Gnat.headers(), GenServer.from() | nil}

  @typedoc "A PubAck body read as an outcome, independent of which wire asked."
  @type outcome :: :ok | :rejected | :ambiguous

  @typedoc """
  Immutable per-shard context. Built once by the collector at init and passed
  to every wire call; nothing in it changes over the shard's life.
  """
  @type t :: %__MODULE__{
          conn: GenServer.server(),
          table: Pending.table(),
          counter: Pending.counter(),
          prefix: String.t(),
          batch_token: binary(),
          senders: [pid()],
          max_attempts: pos_integer(),
          call_timeout_ms: pos_integer()
        }

  defstruct [
    :conn,
    :table,
    :counter,
    :prefix,
    :batch_token,
    :senders,
    :max_attempts,
    :call_timeout_ms
  ]

  @doc "Stages a flushed cohort into pending rows and writes it to the connection."
  @callback send_cohort([entry()], t()) :: :ok

  @doc "Applies one reply that `AckPath.parse/2` routed to this wire."
  @callback ack(Ingress.Nats.Publisher.AckPath.ref(), binary(), t()) :: :ok

  @doc "Resolves a pending row of this wire's shape that outlived the ack deadline."
  @callback expire(Pending.row(), t()) :: :ok

  ## Shared wire helpers

  @doc """
  Reads a PubAck body as an outcome. `:rejected` is the only definite negative:
  the broker answered and stored nothing. A malformed or unparseable reply is
  `:ambiguous` and fails closed, exactly as a lost ack does.
  """
  @spec classify(binary()) :: outcome()
  def classify(body) do
    case Nats.parse_pub_ack(body) do
      :ok -> :ok
      {:error, {:pub_ack, _reason}} -> :rejected
      {:error, _reason} -> :ambiguous
    end
  end

  @doc "Splits a queued entry into its wire parts, normalising the two shapes."
  @spec parts(entry()) :: {String.t(), iodata(), Gnat.headers()}
  def parts({subject, json, _from}), do: {subject, json, []}
  def parts({subject, json, trace_headers, _from}), do: {subject, json, trace_headers}

  @spec pub(GenServer.server(), String.t(), iodata(), String.t(), Gnat.headers(), pos_integer()) ::
          :ok | {:error, term()}
  def pub(conn, subject, json, reply, trace_headers, timeout),
    do: safe_pub(conn, subject, json, publish_opts([reply_to: reply], trace_headers), timeout)

  @spec publish_opts(keyword(), Gnat.headers()) :: keyword()
  def publish_opts(opts, []), do: opts
  def publish_opts(opts, headers), do: Keyword.put(opts, :headers, headers)

  @doc """
  One wire write, bounded by `timeout` and never allowed to raise.

  `Gnat.pub/4` hardcodes the 5s `GenServer.call` default, which outlives the
  publisher's whole ack budget: while the collector blocks in it, it applies no
  PubAcks and runs no sweep ticks, so a single wedged socket write turns into a
  window-wide mass expiry the moment it returns. This is the same call with the
  timeout supplied — the private header preparation and the `[:gnat, :pub]`
  telemetry are reproduced so nothing downstream can tell the two apart.

  Every failure — an unregistered or dead connection, a write that outlives
  `timeout` — comes back as `{:error, :not_connected}`. That is a *definite*
  "the message never left the VM", so callers resolve the row immediately
  instead of holding a slot until the sweep.
  """
  @spec safe_pub(GenServer.server(), String.t(), iodata(), keyword(), pos_integer()) ::
          :ok | {:error, term()}
  def safe_pub(conn, subject, json, opts, timeout) do
    started = :erlang.monotonic_time()
    result = GenServer.call(conn, {:pub, subject, json, prepare_headers(opts)}, timeout)
    latency = :erlang.monotonic_time() - started
    :telemetry.execute([:gnat, :pub], %{latency: latency}, %{topic: subject})
    result
  catch
    :exit, _ -> {:error, :not_connected}
  end

  # Mirrors Gnat's own private prepare_headers/1: cowlib-encode the header list
  # and re-put it, because Gnat.Command.build/4 matches a literal
  # `[headers: _, reply_to: _]` and only Keyword.put's prepend produces that
  # order. cowlib arrives with :gnat; encoding here is what lets the call be
  # bounded at all.
  defp prepare_headers(opts) do
    case Keyword.fetch(opts, :headers) do
      {:ok, headers} -> Keyword.put(opts, :headers, :cow_http.headers(headers))
      :error -> opts
    end
  end

  @spec reply(GenServer.from() | nil, term()) :: :ok
  def reply(nil, _result), do: :ok
  def reply(from, result), do: GenServer.reply(from, result)

  @doc """
  Samples PubAck latency. One row in `@ack_latency_sample_rate` is measured;
  a cohort contributes its whole count to the sampled bucket.
  """
  @spec record_ack_latency(pos_integer(), integer(), pos_integer()) :: :ok
  def record_ack_latency(id, timestamp, count)
      when rem(id, @ack_latency_sample_rate) == 0 do
    bucket = ack_latency_bucket(max(Pending.now_ms() - timestamp, 0))
    Metrics.count("Nats/PubAckLatency/#{bucket}", count)
    Metrics.count("Nats/PubAckLatency/Sampled", count)
  end

  def record_ack_latency(_id, _timestamp, _count), do: :ok

  defp ack_latency_bucket(ms) when ms <= 1, do: "Le1ms"
  defp ack_latency_bucket(ms) when ms <= 2, do: "Le2ms"
  defp ack_latency_bucket(ms) when ms <= 4, do: "Le4ms"
  defp ack_latency_bucket(ms) when ms <= 8, do: "Le8ms"
  defp ack_latency_bucket(ms) when ms <= 16, do: "Le16ms"
  defp ack_latency_bucket(ms) when ms <= 32, do: "Le32ms"
  defp ack_latency_bucket(ms) when ms <= 64, do: "Le64ms"
  defp ack_latency_bucket(ms) when ms <= 128, do: "Le128ms"
  defp ack_latency_bucket(_ms), do: "Gt128ms"
end
