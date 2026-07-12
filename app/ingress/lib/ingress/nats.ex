defmodule Ingress.Nats do
  @moduledoc """
  Outbound NATS publishing of the twitch.ingress.* firehose, in two
  disciplines:

    * `publish_acked/3` — lane events (the traffic that must not be silently
      lost). An asynchronous, pipelined JetStream publish (see
      `Ingress.Nats.Publisher`): the event goes on the wire immediately carrying
      a private reply subject and a `Nats-Msg-Id`, and its PubAck is reconciled
      later by a single ack multiplexer. Up to `publish_max_pending` publishes
      are outstanding at once, so a slow broker no longer parks a worker per
      event; instead the window fills and further publishes are shed at that
      bound. The `Nats-Msg-Id` collapses retries and Twitch's own EventSub
      redeliveries into one stored copy, so re-publishing a missing/errored ack
      is safe.

    * `publish/2` — status/telemetry events. Fire-and-forget core publish; if
      the connection is down the message is dropped with a warning. We prefer
      drop over unbounded buffering.

  Publishes ride the BUS-account connection (`:gnat_bus`) because the
  twitch.ingress.event/status.* subjects are captured by the JetStream streams,
  which live in the shared BUS account; the RPC connection (`:gnat`) carries only
  the twitch_ingress account's request/reply traffic.
  """

  require Logger

  alias Ingress.{Metrics, Nats.Publisher}

  @connection :gnat_bus

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
  Asynchronous JetStream publish with broker-side dedup.

  `dedup_id` becomes the message's `Nats-Msg-Id`: within the stream's
  duplicate window the broker stores the first copy and acks the rest as
  duplicates, which makes re-publishing on a missing PubAck safe and absorbs
  Twitch's at-least-once EventSub redeliveries without any consumer work.
  `nil` skips the header (no dedup) but the publish is still ack-tracked.

  Returns `:ok` once the event is on the wire (its PubAck is now awaited
  asynchronously by `Ingress.Nats.Publisher`), or `{:error, reason}` when the
  event was dropped at admission — `:overloaded` when the in-flight window is
  full, `:not_connected` when the publisher or its BUS connection is down.
  """
  @spec publish_acked(String.t(), map(), String.t() | nil) :: :ok | {:error, term()}
  def publish_acked(subject, payload, dedup_id) do
    json = Jason.encode!(payload)

    case Publisher.enqueue(subject, json, dedup_id) do
      :ok ->
        :ok

      {:error, :overloaded} = error ->
        Metrics.count("Nats/PublishDropped")
        Metrics.count("Nats/PublishOverloaded")
        error

      {:error, :not_connected} = error ->
        Logger.warning("NATS publisher unavailable; dropping message on #{subject}")
        Metrics.count("Nats/PublishDropped")
        error

      {:error, _reason} = error ->
        Metrics.count("Nats/PublishDropped")
        error
    end
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
