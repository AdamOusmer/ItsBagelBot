defmodule Ingress.Nats do
  @moduledoc """
  Outbound NATS publishing of the twitch.ingress.* firehose, in two
  disciplines:

    * `publish_acked/3` — lane events (the traffic that must not be silently
      lost). A JetStream publish: the request blocks on the broker's PubAck,
      retries a bounded number of times, and carries a `Nats-Msg-Id` so the
      stream's duplicate window collapses retries and Twitch's own EventSub
      redeliveries into one stored copy. Callers are dispatcher workers, so a
      slow broker occupies pool slots and the dispatcher sheds at its bound —
      the intended backpressure — instead of a fast, invisible black hole.

    * `publish/2` — status/telemetry events. Fire-and-forget core publish; if
      the connection is down the message is dropped with a warning. We prefer
      drop over unbounded buffering.

  Publishes ride the BUS-account connection (`:gnat_bus`) because the
  twitch.ingress.event/status.* subjects are captured by the JetStream streams,
  which live in the shared BUS account; the RPC connection (`:gnat`) carries only
  the twitch_ingress account's request/reply traffic.
  """

  require Logger

  alias Ingress.{Config, Metrics}

  @connection :gnat_bus
  # Pause between PubAck retry attempts: long enough to ride out a broker
  # hiccup, short enough that a dispatcher worker slot is never parked long.
  @retry_backoff_ms 250

  @spec publish(String.t(), map()) :: :ok | {:error, term()}
  def publish(subject, payload) do
    json = Jason.encode!(payload)

    case Process.whereis(@connection) do
      nil ->
        Logger.warning("NATS connection down; dropping message on #{subject}")
        Metrics.count("Nats/PublishDropped")
        {:error, :not_connected}

      _pid ->
        Gnat.pub(@connection, subject, json)
    end
  end

  @doc """
  JetStream-acked publish with broker-side dedup.

  `dedup_id` becomes the message's `Nats-Msg-Id`: within the stream's
  duplicate window the broker stores the first copy and acks the rest as
  duplicates, which makes retrying on a missing PubAck safe and absorbs
  Twitch's at-least-once EventSub redeliveries without any consumer work.
  `nil` skips the header (no dedup) but still waits for the ack.

  A connection that is down drops immediately — the connection supervisor's
  reconnect backoff is longer than the whole retry budget, so spinning here
  could never succeed and would only park the worker.
  """
  @spec publish_acked(String.t(), map(), String.t() | nil) :: :ok | {:error, term()}
  def publish_acked(subject, payload, dedup_id) do
    json = Jason.encode!(payload)
    attempt(subject, json, request_opts(dedup_id), 1)
  end

  defp request_opts(nil), do: [receive_timeout: Config.publish_ack_timeout_ms()]

  defp request_opts(dedup_id) do
    [
      receive_timeout: Config.publish_ack_timeout_ms(),
      headers: [{"Nats-Msg-Id", dedup_id}]
    ]
  end

  defp attempt(subject, json, opts, n) do
    case request_ack(subject, json, opts) do
      :ok ->
        :ok

      {:error, :not_connected} = error ->
        Logger.warning("NATS connection down; dropping message on #{subject}")
        Metrics.count("Nats/PublishDropped")
        error

      {:error, reason} = error ->
        if n < Config.publish_attempts() do
          Metrics.count("Nats/PublishRetried")
          Process.sleep(@retry_backoff_ms)
          attempt(subject, json, opts, n + 1)
        else
          Logger.warning("publish on #{subject} failed after #{n} attempts: #{inspect(reason)}")
          Metrics.count("Nats/PublishFailed")
          error
        end
    end
  end

  defp request_ack(subject, json, opts) do
    case Gnat.request(@connection, subject, json, opts) do
      {:ok, %{body: body}} -> parse_pub_ack(body)
      {:error, reason} -> {:error, reason}
    end
  catch
    # The connection process is gone (registered name unbound, or it died
    # mid-call). Its supervisor is already reconnecting on its own backoff.
    :exit, _ -> {:error, :not_connected}
  end

  @doc false
  # A JetStream PubAck: {"stream": ..., "seq": N} on success, additionally
  # "duplicate": true when the id was already stored inside the duplicate
  # window (success for our purposes — the event is in the stream exactly
  # once), or {"error": {...}} when storage refused the message.
  @spec parse_pub_ack(binary()) :: :ok | {:error, term()}
  def parse_pub_ack(body) do
    case Jason.decode(body) do
      {:ok, %{"error" => error}} ->
        {:error, {:pub_ack, error}}

      {:ok, %{"duplicate" => true}} ->
        Metrics.count("Nats/PublishDeduped")
        :ok

      {:ok, %{"seq" => _seq}} ->
        :ok

      _other ->
        {:error, :bad_pub_ack}
    end
  end
end
