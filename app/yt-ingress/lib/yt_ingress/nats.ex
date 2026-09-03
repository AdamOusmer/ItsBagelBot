# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule YtIngress.Nats do
  @moduledoc """
  Outbound NATS publishing of the youtube.ingress.* firehose, in two
  disciplines:

    * `publish_event/2` — lane events (the traffic that must not be silently
      lost). Each publish is a JetStream publish: the subject is captured by
      the lane streams and the PubAck comes back on a one-shot inbox. At
      YouTube chat rates (orders of magnitude below the Twitch firehose) a
      synchronous per-event ack costs nothing and needs none of the pooled
      publisher machinery. A missing or ambiguous ack is NOT retried — the
      broker may already have stored the event, so retrying risks duplicates;
      it degrades to core-NATS at-most-once semantics with a metric. A
      definite negative ack is surfaced to the caller.

    * `publish/2` — status/telemetry events. Fire-and-forget; if the
      connection is down or the payload will not encode, the message is
      dropped into batched counters. We prefer drop over unbounded buffering
      and never produce one log per event during an outage.

  Publishes ride the BUS-account connection (`:gnat_bus`) because the
  youtube.ingress.event/status.* subjects are captured by the JetStream
  streams, which live in the shared BUS account; the RPC connection (`:gnat`)
  carries only request/reply traffic.
  """

  alias YtIngress.{JSON, Metrics}

  @connection :gnat_bus
  # JetStream PubAck wait. Generous enough for a quorum write on a healthy
  # hub; past it the publish degrades to at-most-once rather than stalling
  # the chat reader.
  @ack_timeout_ms 2_000

  @spec publish_event(String.t(), map()) :: :ok | {:error, term()}
  def publish_event(subject, payload) do
    case whereis_connection() do
      nil ->
        Metrics.count("Nats/PublishDropped")
        Metrics.count("Nats/PublishNotConnected")
        {:error, :not_connected}

      pid ->
        inbox = ack_inbox()

        case Gnat.sub(pid, self(), inbox) do
          {:ok, sid} ->
            # Gnat keys receivers by integer sid; unsub(inbox-string) is a
            # no-op and leaked a subscription per lane event. Hold the sid
            # and always drop it, including on encode failure / timeout.
            try do
              publish_and_await_ack(subject, payload, inbox)
            after
              Gnat.unsub(pid, sid)
            end

          {:error, reason} ->
            Metrics.count("Nats/PublishDropped")
            {:error, {:subscribe, reason}}
        end
    end
  end
  @spec publish(String.t(), map()) :: :ok | {:error, term()}
  def publish(subject, payload) do
    case safe_encode(payload) do
      {:ok, json} ->
        case whereis_connection() do
          nil ->
            Metrics.count("Nats/PublishDropped")
            Metrics.count("Nats/PublishNotConnected")
            {:error, :not_connected}

          pid ->
            Gnat.pub(pid, subject, json)
        end

      {:error, reason} ->
        # Status/telemetry is fire-and-forget: an unencodable payload must never
        # crash the session that emitted it.
        Metrics.count("Nats/PublishDropped")
        Metrics.count("Nats/PublishEncodeError")
        {:error, {:encode, reason}}
    end
  end

  defp publish_and_await_ack(subject, payload, inbox) do
    case JSON.encode(payload) do
      json when is_list(json) ->
        Gnat.pub(@connection, subject, json, reply_to: inbox)

        result =
          receive do
            {:msg, %{body: body, topic: ^inbox}} ->
              parse_pub_ack(body)

              # Anything else on our inbox that is not a PubAck shape parses
              # as bad_pub_ack above.
          after
            @ack_timeout_ms ->
              # Ambiguous: the broker may have stored it. Degrade to
              # at-most-once, never retry.
              Metrics.count("Nats/PublishAckTimeout")
              :ok
          end

        result

      {:error, _reason} = error ->
        # An unencodable lane payload is a programming error upstream; drop it
        # here rather than crash the chat reader mid-stream.
        Metrics.count("Nats/PublishDropped")
        Metrics.count("Nats/PublishEncodeError")
        error
    end
  end

  @doc """
  A JetStream PubAck: {"stream": ..., "seq": N} on success; an {"error": {...}}
  response means storage refused the message.
  """
  @spec parse_pub_ack(binary()) :: :ok | {:error, term()}
  def parse_pub_ack(body) do
    cond do
      :binary.match(body, ~s("error")) != :nomatch ->
        case JSON.decode(body) do
          {:ok, %{"error" => error}} -> {:error, {:pub_ack, error}}
          _ -> {:error, :bad_pub_ack}
        end

      :binary.match(body, ~s("seq":)) == :nomatch ->
        # No JetStream responder answered (dev server without JetStream): the
        # message still went out as a core NATS publish.
        Metrics.count("Nats/PublishUnacked")
        :ok

      true ->
        :ok
    end
  end

  defp safe_encode(payload) do
    {:ok, JSON.encode(payload)}
  rescue
    error -> {:error, error}
  catch
    kind, reason -> {:error, {kind, reason}}
  end

  # A connection's registered name exists only while it is established
  # (Gnat.ConnectionSupervisor restarts it unregistered during reconnects), so
  # Process.whereis/1 is a truthful zero-cost connectivity probe.
  defp whereis_connection, do: Process.whereis(@connection)

  defp ack_inbox, do: "_INBOX." <> Base.encode16(:crypto.strong_rand_bytes(12))
end
