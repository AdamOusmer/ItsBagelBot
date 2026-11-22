# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlays.Delivery do
  require Logger

  alias ControlsGateway.Overlays.{Ack, Tiers}
  alias ControlsGateway.{Subjects, Tts.Session}
  alias Platform.Nats
  alias Platform.Nats.Planes

  @offer_timeout_ms 2_000
  @backoff_ns [500_000_000, 2_000_000_000, 5_000_000_000, 10_000_000_000]
  @backoff_cap_ns 30_000_000_000
  @kind ~r/\A[a-z]+\z/

  def handle(%{topic: topic, body: body, reply_to: ack} = message) when is_binary(ack) do
    with {:ok, route} <- route(topic),
         {:ok, %{} = data} <- Jason.decode(body),
         {:ok, %{num_delivered: delivered}} <- Planes.jetstream_metadata(message) do
      offer(ack, route, data, delivered)
    else
      _ ->
        Logger.warning("overlay message invalid", reason: :invalid_message)
        Ack.term(ack)
    end
  end

  def handle(_message), do: :ok

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

  defp offer(ack, {user_id, kind}, data, delivered) do
    request = Jason.encode!(%{ack: ack, kind: kind, data: data, delivered: delivered})

    if accepted?(ask(Subjects.overlay_session(user_id), request)) do
      :ok
    else
      Logger.info("overlay clip requeued", user_id: user_id, event_id: data["event_id"])
      Ack.nak(ack, delay_ns(delivered))
    end
  end

  defp ask(subject, body) do
    reply = Subjects.overlay_offer_reply()

    with %Planes{pid: pid} = conn <- Planes.connection(:jetstream),
         {:ok, sid} <- Planes.subscribe(conn, self(), reply) do
      Nats.publish(pid, subject, body, reply_to: reply)
      answer = await_reply(sid)
      Planes.unsubscribe(conn, sid)
      answer
    end
  end

  defp await_reply(sid) do
    receive do
      {:msg, %{sid: ^sid, body: body}} -> {:ok, body}
    after
      @offer_timeout_ms -> :timeout
    end
  end

  defp accepted?({:ok, body}), do: Jason.decode(body) == {:ok, %{"accepted" => true}}
  defp accepted?(_reply), do: false

  defp delay_ns(delivered), do: Enum.at(@backoff_ns, delivered - 1, @backoff_cap_ns)
end
