defmodule Ingress.Nats do
  @moduledoc """
  Outbound NATS publishing of the twitch.ingress.* firehose, in two
  disciplines:

    * `publish_acked/2` — lane events (the traffic that must not be silently
      lost). Scheduler-local cohorts publish through Gnat with bounded PubAck
      tracking. Ingress deliberately does not attach `Nats-Msg-Id`: EventSub
      websocket delivery is not replayed and the broker-side dedup index is a
      material ingest tax. An ambiguous missing ack is therefore dropped rather
      than retried, preserving at-most-once behavior instead of risking a
      duplicate. Up to `publish_max_pending` events may be outstanding; the
      explicit bound sheds overload instead of growing memory without limit.

    * `publish/2` — status/telemetry events. Fire-and-forget core publish; if
      the connection is down the message is dropped into batched counters. We
      prefer drop over unbounded buffering and never produce one log per event
      during an outage.

  Publishes ride the BUS-account connection (`:gnat_bus`) because the
  twitch.ingress.event/status.* subjects are captured by the JetStream streams,
  which live in the shared BUS account; the RPC connection (`:gnat`) carries only
  the twitch_ingress account's request/reply traffic.
  """

  alias Ingress.{JSON, Metrics, Nats.Publisher, Trace}

  @connection :gnat_bus

  @spec publish(String.t(), map()) :: :ok | {:error, term()}
  def publish(subject, payload) do
    case safe_encode(payload) do
      {:ok, json} ->
        case Process.whereis(@connection) do
          nil ->
            Metrics.count("Nats/PublishDropped")
            Metrics.count("Nats/PublishNotConnected")
            {:error, :not_connected}

          _pid ->
            Gnat.pub(@connection, subject, json)
        end

      {:error, reason} ->
        # Status/telemetry is fire-and-forget: an unencodable payload must never
        # crash the shard that emitted it. A DateTime tuple field once raised here
        # and cascaded into a shard restart storm that wedged the whole rollout.
        Metrics.count("Nats/PublishDropped")
        Metrics.count("Nats/PublishEncodeError")
        {:error, {:encode, reason}}
    end
  end

  # Encoding must not propagate as an exit from the caller (see publish/2). Lane
  # events (publish_acked) stay strict — their payloads are decoded Twitch maps.
  defp safe_encode(payload) do
    {:ok, JSON.encode(payload)}
  rescue
    error -> {:error, error}
  catch
    kind, reason -> {:error, {kind, reason}}
  end

  @doc """
  Bounded, dedup-free JetStream publish cohorts.

  Ingress never attaches `Nats-Msg-Id`. The publish remains ack-tracked, but an
  ambiguous timeout is not retried because the broker may already have stored
  the event. Definite negative PubAcks can still be retried safely.

  Returns `:ok` once the event is on the wire (its PubAck is now awaited
  asynchronously by `Ingress.Nats.Publisher`), or `{:error, reason}` when the
  event was dropped at admission — `:overloaded` when the in-flight window is
  full, `:not_connected` when the publisher or its BUS connection is down.
  """
  @spec publish_acked(String.t(), map()) :: :ok | {:error, term()}
  def publish_acked(subject, payload) do
    # OTP JSON, Gnat and the TCP/SSL transports all keep this as iodata, so no
    # flattened copy is made before the socket write or PubAck retry storage.
    json =
      Trace.span("encode", fn ->
        JSON.encode(payload)
      end)

    result =
      Trace.span(
        "nats.publish.admit",
        ["messaging.destination": Trace.destination(subject)],
        fn ->
          result = Publisher.enqueue(subject, json, Trace.trace_headers())
          Trace.add_span_attributes(result: Trace.result(result))
          result
        end
      )

    case result do
      :ok ->
        :ok

      {:error, :overloaded} = error ->
        Metrics.count("Nats/PublishDropped")
        Metrics.count("Nats/PublishOverloaded")
        error

      {:error, :not_connected} = error ->
        Metrics.count("Nats/PublishDropped")
        Metrics.count("Nats/PublishNotConnected")
        error

      {:error, _reason} = error ->
        Metrics.count("Nats/PublishDropped")
        error
    end
  end

  @doc false
  # A JetStream PubAck: {"stream": ..., "seq": N} on success, additionally
  # "duplicate": true is still accepted for rolling-upgrade compatibility,
  # though the dedup-free ingress publisher no longer causes that response.
  # An {"error": {...}} response means storage refused the message.
  @spec parse_pub_ack(binary()) :: :ok | {:error, term()}
  def parse_pub_ack(body) do
    cond do
      # Successful JetStream PubAcks are compact JSON emitted by nats-server.
      # Recognizing their stable fields avoids allocating a map and decoding
      # JSON for every event on the ack collector's hottest path.
      :binary.match(body, ~s("error")) != :nomatch ->
        case JSON.decode(body) do
          {:ok, %{"error" => error}} -> {:error, {:pub_ack, error}}
          _ -> {:error, :bad_pub_ack}
        end

      :binary.match(body, ~s("seq":)) == :nomatch ->
        {:error, :bad_pub_ack}

      :binary.match(body, ~s("duplicate":true)) != :nomatch ->
        Metrics.count("Nats/PublishDeduped")
        :ok

      true ->
        :ok
    end
  end
end
