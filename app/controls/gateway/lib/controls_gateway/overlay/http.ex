# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Http do
  require Logger

  alias ControlsGateway.{Http, OverlayRequest}
  alias ControlsGateway.Overlay.{Nats, Runtime, Sandbox, Session}
  alias ControlsGateway.Overlays.Acks
  alias ControlsGateway.Powered.Page

  @headers %{
    "cache-control" => "no-store",
    "referrer-policy" => "no-referrer",
    "x-content-type-options" => "nosniff"
  }

  def handle(%{method: "GET"} = conn, :page), do: Page.reply_overlay(conn)

  def handle(%{method: "GET"} = conn, :runtime),
    do: Runtime.reply(conn, conn.path_params["name"])

  def handle(%{method: "GET"} = conn, :sandbox),
    do: Sandbox.reply(conn, conn.path_params["widget"])

  def handle(%{method: "POST"} = conn, :session), do: exchange(conn)

  def handle(%{method: "GET"} = conn, :doc), do: authorized(conn, :doc)

  def handle(%{method: "POST"} = conn, route) when route in [:ack, :progress],
    do: authorized(conn, route)

  def handle(conn, route),
    do: Http.reply(conn, 405, Map.put(@headers, "allow", allowed(route)))

  defp allowed(route) when route in [:session, :ack, :progress], do: "POST"
  defp allowed(_route), do: "GET"

  defp authorized(conn, route) do
    case Session.read(conn) do
      {:ok, session} -> serve(route, conn, session)
      :error -> Http.reply(conn, 401, @headers)
    end
  end

  defp serve(:doc, conn, session), do: serve_doc(conn, session)
  defp serve(:ack, conn, session), do: Acks.ack(conn, session, @headers)
  defp serve(:progress, conn, session), do: Acks.progress(conn, session, @headers)

  defp serve_doc(conn, session) do
    case Nats.check(session) do
      {:ok, overlay} ->
        Http.json(conn, 200, @headers, %{
          document: overlay.document,
          revision: overlay.revision,
          premium: overlay.premium
        })

      {:denied, reason} ->
        Logger.info("Overlay document refused", overlay_id: session.overlay_id, reason: reason)
        Http.json(conn, 403, @headers, %{reason: reason})

      :unavailable ->
        Http.reply(conn, 503, @headers)
    end
  end

  defp exchange(conn) do
    {hashed, conn} = OverlayRequest.token_hash(conn)

    case with(
           {:ok, hash} <- hashed,
           {:ok, overlay} <- Nats.resolve(hash),
           do: {:ok, {overlay, hash}}
         ) do
      {:ok, {overlay, hash}} ->
        Logger.info("Overlay session opened", overlay_id: overlay.overlay_id)
        conn |> Session.put(overlay, hash) |> Http.reply(204, @headers)

      {:not_found, reason} ->
        Logger.warning("Overlay session rejected", reason: reason)
        Http.reply(conn, 404, @headers)

      {:unavailable, reason} ->
        Logger.warning("Overlay session resolution unavailable", reason: reason)
        Http.reply(conn, 503, @headers)
    end
  end
end
