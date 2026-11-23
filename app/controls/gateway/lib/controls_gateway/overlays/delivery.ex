# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlays.Delivery do
  require Logger

  alias ControlsGateway.{Metrics, Overlays}
  alias ControlsGateway.Overlays.Tiers
  alias ControlsGateway.{Subjects, Tts.Session}
  alias Platform.Nats.{Ack, Planes, Requester}

  @offer_timeout_ms 2_000
  @backoff_ns [250_000_000, 1_000_000_000, 2_000_000_000, 5_000_000_000]
  @backoff_cap_ns 10_000_000_000
  @alert_ttl_s 300
  @kind ~r/\A[a-z]+\z/
  @offer_metrics %{
    accepted: "Offer/Accepted",
    no_responders: "Offer/NoResponders",
    not_connected: "Offer/NotConnected",
    timeout: "Offer/Timeout",
    refused: "Offer/Refused"
  }

  def handle(%{topic: topic, body: body, reply_to: ack} = message) when is_binary(ack) do
    with {:ok, route} <- route(topic),
         {:ok, %{} = data} <- Jason.decode(body),
         {:ok, metadata} <- Planes.jetstream_metadata(message) do
      offer(ack, route, data, metadata)
    else
      _ ->
        Logger.warning("overlay message invalid", reason: :invalid_message)
        Metrics.count("Ack/Term")
        count_failure(Ack.term(:jetstream, ack))
    end
  end

  def handle(_message), do: :ok

  def offer_timeout_ms, do: @offer_timeout_ms

  def expires_at(%DateTime{} = published_at),
    do: published_at |> DateTime.add(@alert_ttl_s) |> DateTime.to_iso8601()

  defp route(topic) do
    with ["overlays", tier, kind, id] <- String.split(topic, "."),
         true <- Tiers.tier(tier) != nil,
         true <- Regex.match?(@kind, kind),
         {user_id, ""} <- Integer.parse(id),
         true <- Session.valid_user_id?(user_id) do
      {:ok, {user_id, kind}}
    else
      _ -> :error
    end
  end

  defp offer(ack, {user_id, kind}, data, metadata) do
    delivered = metadata.num_delivered

    request =
      Jason.encode!(%{
        ack: ack,
        kind: kind,
        data: data,
        delivered: delivered,
        expires_at: expires_at(metadata.timestamp)
      })

    outcome = outcome(ask(Subjects.overlay_session(user_id), request))
    Metrics.count(Map.fetch!(@offer_metrics, outcome))
    settle(outcome, ack, {user_id, data["event_id"], delivered})
  end

  defp settle(:accepted, _ack, _clip), do: :ok

  defp settle(_outcome, ack, {user_id, event_id, delivered}) do
    Logger.info("overlay clip requeued", user_id: user_id, event_id: event_id)
    Metrics.count("Ack/Nak")
    count_failure(Ack.nak(:jetstream, ack, delay_ns(delivered)))
  end

  defp count_failure(:ok), do: :ok
  defp count_failure(_result), do: Metrics.count("Ack/Failed")

  defp ask(subject, body),
    do: Requester.request(Overlays.offers(), subject, body, @offer_timeout_ms)

  defp outcome({:ok, body}),
    do: if(Jason.decode(body) == {:ok, %{"accepted" => true}}, do: :accepted, else: :refused)

  defp outcome({:error, :timeout}), do: :timeout
  defp outcome({:error, :not_connected}), do: :not_connected
  defp outcome({:error, _reason}), do: :no_responders

  defp delay_ns(delivered), do: Enum.at(@backoff_ns, delivered - 1, @backoff_cap_ns)
end
