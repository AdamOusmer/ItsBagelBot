# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Overlay.Http do
  @behaviour :cowboy_loop
  require Logger

  alias ControlsGateway.{Drain, OverlayRequest}
  alias ControlsGateway.Overlay.{Events, Nats, Session}
  alias ControlsGateway.Powered.Page
  alias ControlsGateway.Tts.Throttle

  @headers %{
    "cache-control" => "no-store",
    "referrer-policy" => "no-referrer",
    "x-content-type-options" => "nosniff"
  }
  @drain_retry_after "5"

  @impl true
  def init(req, route), do: dispatch(:cowboy_req.method(req), route, req)

  @impl true
  defdelegate info(message, req, state), to: Events

  defp dispatch("GET", :page, req), do: {:ok, Page.reply(req), nil}
  defp dispatch("POST", :session, req), do: exchange(req)
  defp dispatch("GET", :doc, req), do: authorized(:doc, req)

  defp dispatch("GET", :events, req) do
    case Drain.track_connection() do
      :ok -> authorized(:events, req)
      :draining -> reply(req, 503, Map.put(@headers, "retry-after", @drain_retry_after))
    end
  end

  defp dispatch(_method, route, req),
    do: reply(req, 405, Map.put(@headers, "allow", allowed(route)))

  defp allowed(:session), do: "POST"
  defp allowed(_route), do: "GET"

  defp authorized(route, req) do
    case Session.read(req) do
      {:ok, session} -> serve(route, req, session)
      :error -> reply(req, 401)
    end
  end

  defp serve(:events, req, session), do: Events.open(req, session, @headers)

  defp serve(:doc, req, session) do
    case Nats.check(session) do
      {:ok, overlay} ->
        json(req, 200, %{
          document: overlay.document,
          revision: overlay.revision,
          premium: overlay.premium
        })

      {:denied, reason} ->
        json(req, 403, %{reason: reason})

      :unavailable ->
        reply(req, 503)
    end
  end

  defp exchange(req) do
    case Throttle.reserve(OverlayRequest.client_ip(req)) do
      {:ok, reservation} -> exchange(req, reservation)
      :limited -> reply(req, 429, Map.put(@headers, "retry-after", "60"))
    end
  end

  defp exchange(req, reservation) do
    {hashed, req} = OverlayRequest.token_hash(req)

    case with({:ok, hash} <- hashed, {:ok, overlay} <- Nats.resolve(hash), do: {overlay, hash}) do
      {overlay, hash} when is_map(overlay) ->
        Throttle.refund(reservation)
        Logger.info("Overlay session opened", overlay_id: overlay.overlay_id)
        req |> Session.put(overlay, hash) |> reply(204)

      {:not_found, reason} ->
        Logger.warning("Overlay session rejected", reason: reason)
        reply(req, 404)

      {:unavailable, reason} ->
        Throttle.refund(reservation)
        Logger.warning("Overlay session resolution unavailable", reason: reason)
        reply(req, 503)
    end
  end

  defp json(req, status, body) do
    headers = Map.put(@headers, "content-type", "application/json")
    {:ok, :cowboy_req.reply(status, headers, Jason.encode!(body), req), nil}
  end

  defp reply(req, status, headers \\ @headers),
    do: {:ok, :cowboy_req.reply(status, headers, req), nil}
end
