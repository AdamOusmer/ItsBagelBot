# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule Ingress.Nats.Publisher.Wire do
  alias Ingress.{Metrics, Nats}
  alias Ingress.Nats.Publisher.Pending

  @ack_latency_sample_rate 64

  @typedoc """
  One queued event; the wider tuple carries trace headers. `from` is reserved
  and always `nil`: a wire that populates it must `reply/2` exactly once on
  every terminal path, or the caller blocks for its full timeout.
  """
  @type entry ::
          {String.t(), iodata(), GenServer.from() | nil}
          | {String.t(), iodata(), Gnat.headers(), GenServer.from() | nil}

  @typedoc "A PubAck body read as an outcome, independent of which wire asked."
  @type outcome :: :ok | :rejected | :ambiguous

  @typedoc "One write: subject, payload and publish options (`:reply_to`, `:headers`)."
  @type message :: {String.t(), iodata(), keyword()}

  @typedoc "Immutable per-shard context built once by the collector."
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

  @callback send_cohort([entry()], t()) :: :ok

  @callback ack(Ingress.Nats.Publisher.AckPath.ref(), binary(), t()) :: :ok

  @callback expire(Pending.row(), t()) :: :ok

  @spec classify(binary()) :: outcome()
  def classify(body) do
    case Nats.parse_pub_ack(body) do
      :ok -> :ok
      {:error, {:pub_ack, _reason}} -> :rejected
      {:error, _reason} -> :ambiguous
    end
  end

  @spec parts(entry()) :: {String.t(), iodata(), Gnat.headers()}
  def parts({subject, json, _from}), do: {subject, json, []}
  def parts({subject, json, trace_headers, _from}), do: {subject, json, trace_headers}

  @spec publish_opts(keyword(), Gnat.headers()) :: keyword()
  def publish_opts(opts, []), do: opts
  def publish_opts(opts, headers), do: Keyword.put(opts, :headers, headers)

  @spec safe_pub(GenServer.server(), message(), pos_integer()) :: :ok | {:error, term()}
  def safe_pub(conn, {subject, json, opts}, timeout) do
    started = :erlang.monotonic_time()
    result = GenServer.call(conn, {:pub, subject, json, prepare_headers(opts)}, timeout)
    latency = :erlang.monotonic_time() - started
    :telemetry.execute([:gnat, :pub], %{latency: latency}, %{topic: subject})
    result
  catch
    :exit, _ -> {:error, :not_connected}
  end

  # Gnat.Command.build/4 matches the exact [headers: _, reply_to: _] order Keyword.put produces.
  defp prepare_headers(opts) do
    case Keyword.fetch(opts, :headers) do
      {:ok, headers} -> Keyword.put(opts, :headers, :cow_http.headers(headers))
      :error -> opts
    end
  end

  @spec reply(GenServer.from() | nil, term()) :: :ok
  def reply(nil, _result), do: :ok
  def reply(from, result), do: GenServer.reply(from, result)

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
