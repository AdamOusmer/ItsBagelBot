# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Http do
  require Logger

  alias ControlsGateway.{Http, OverlayRequest}
  alias ControlsGateway.Overlays.Acks
  alias ControlsGateway.Tts.{Clips, Nats, Page, Session}

  @headers %{"cache-control" => "no-store", "referrer-policy" => "no-referrer"}
  @allow %{session: "POST", ack: "POST", progress: "POST"}

  def handle(%{method: "GET"} = conn, :page), do: Page.reply(conn, @headers)
  def handle(%{method: "POST"} = conn, :session), do: exchange(conn)
  def handle(%{method: "GET"} = conn, :clip), do: authorized(conn, :clip)

  def handle(%{method: "POST"} = conn, route) when route in [:ack, :progress],
    do: authorized(conn, route)

  def handle(conn, route) do
    allow = Map.get(@allow, route, "GET")
    Http.reply(conn, 405, Map.put(@headers, "allow", allow))
  end

  defp authorized(conn, route) do
    case Session.read(conn) do
      {:ok, session} -> serve(route, conn, session)
      :error -> Http.reply(conn, 401, @headers)
    end
  end

  defp serve(:clip, conn, session), do: Clips.reply(conn, session, @headers)
  defp serve(:ack, conn, session), do: Acks.ack(conn, session, @headers)
  defp serve(:progress, conn, session), do: Acks.progress(conn, session, @headers)

  defp exchange(conn) do
    {hashed, conn} = OverlayRequest.token_hash(conn)

    case with({:ok, hash} <- hashed, do: Nats.resolve(hash)) do
      {:ok, user_id, generation} ->
        Logger.info("TTS session opened", user_id: user_id)
        conn |> Session.put(user_id, generation) |> Http.reply(204, @headers)

      {:not_found, reason} ->
        Logger.warning("TTS session rejected", reason: reason)
        Http.reply(conn, 404, @headers)

      {:unavailable, reason} ->
        Logger.warning("TTS session resolution unavailable", reason: reason)
        Http.reply(conn, 503, @headers)
    end
  end
end
