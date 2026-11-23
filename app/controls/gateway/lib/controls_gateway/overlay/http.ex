# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Http do
  require Logger

  alias ControlsGateway.{Http, OverlayRequest}
  alias ControlsGateway.Overlay.{Nats, Session}
  alias ControlsGateway.Overlays.Acks
  alias ControlsGateway.Powered.Page

  @headers %{
    "cache-control" => "no-store",
    "referrer-policy" => "no-referrer",
    "x-content-type-options" => "nosniff"
  }
  @labels %{
    session: {:warning, "Overlay session rejected", "Overlay session resolution unavailable"},
    config: {:debug, "Powered overlay config rejected", "Powered overlay config unavailable"}
  }

  def handle(%{method: "GET"} = conn, :page), do: Page.reply(conn)

  def handle(%{method: "POST"} = conn, route) when route in [:session, :config],
    do: exchange(conn, route)

  def handle(%{method: "GET"} = conn, :doc), do: authorized(conn, :doc)

  def handle(%{method: "POST"} = conn, route) when route in [:ack, :progress],
    do: authorized(conn, route)

  def handle(conn, route),
    do: Http.reply(conn, 405, Map.put(@headers, "allow", allowed(route)))

  defp allowed(route) when route in [:session, :config, :ack, :progress], do: "POST"
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

  defp exchange(conn, kind) do
    {hashed, conn} = OverlayRequest.token_hash(conn)
    {rejected_level, rejected_message, unavailable_message} = Map.fetch!(@labels, kind)

    case with(
           {:ok, hash} <- hashed,
           {:ok, overlay} <- Nats.resolve(hash),
           do: grant(kind, overlay, hash)
         ) do
      {:ok, result} ->
        granted(conn, kind, result)

      {:not_found, reason} ->
        Logger.log(rejected_level, rejected_message, reason: reason)
        Http.reply(conn, 404, @headers)

      {:unavailable, reason} ->
        Logger.warning(unavailable_message, reason: reason)
        Http.reply(conn, 503, @headers)
    end
  end

  defp grant(:session, overlay, hash), do: {:ok, {overlay, hash}}

  defp grant(
         :config,
         %{allowed: true, document: %{"config" => %{} = config}, premium: premium},
         _hash
       ),
       do: {:ok, %{config: config, premium: premium}}

  defp grant(:config, _overlay, _hash), do: {:not_found, :not_powered}

  defp granted(conn, :session, {overlay, hash}) do
    Logger.info("Overlay session opened", overlay_id: overlay.overlay_id)
    conn |> Session.put(overlay, hash) |> Http.reply(204, @headers)
  end

  defp granted(conn, :config, body), do: Http.json(conn, 200, @headers, body)
end
