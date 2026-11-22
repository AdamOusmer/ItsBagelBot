# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Http do
  @behaviour :cowboy_loop
  require Logger

  alias ControlsGateway.{Drain, OverlayRequest}
  alias ControlsGateway.Overlays.Acks
  alias ControlsGateway.Tts.{Clips, Events, Nats, Page, Session, Throttle}

  @headers %{"cache-control" => "no-store", "referrer-policy" => "no-referrer"}
  @allow %{session: "POST", events: "GET, HEAD", ack: "POST", progress: "POST"}
  @drain_retry_after "5"

  @impl true
  def init(req, route) do
    dispatch(:cowboy_req.method(req), route, req)
  end

  @impl true
  defdelegate info(message, req, state), to: Events

  defp dispatch("GET", :page, req), do: Page.reply(req, @headers)
  defp dispatch("POST", :session, req), do: exchange(req)
  defp dispatch("HEAD", :events, req), do: authorized(:events_status, req)

  defp dispatch("GET", :events, req) do
    case Drain.track_connection() do
      :ok -> authorized(:events, req)
      :draining -> reply(req, 503, Map.put(@headers, "retry-after", @drain_retry_after))
    end
  end

  defp dispatch("GET", :clip, req), do: authorized(:clip, req)
  defp dispatch("POST", route, req) when route in [:ack, :progress], do: authorized(route, req)

  defp dispatch(_method, route, req) do
    allow = Map.get(@allow, route, "GET")
    reply(req, 405, Map.put(@headers, "allow", allow))
  end

  defp authorized(route, req) do
    case Session.read(req) do
      {:ok, session} -> serve(route, req, session)
      :error -> reply(req, 401)
    end
  end

  defp serve(:events, req, session), do: Events.open(req, session, @headers)
  defp serve(:clip, req, session), do: Clips.reply(req, session, @headers)
  defp serve(:ack, req, session), do: Acks.ack(req, session, @headers)
  defp serve(:progress, req, session), do: Acks.progress(req, session, @headers)

  defp serve(:events_status, req, session) do
    case Nats.check(session) do
      true ->
        reply(req, 200)

      false ->
        Logger.info("TTS link no longer valid", user_id: session.user_id)
        reply(req, 204)

      :unavailable ->
        Logger.warning("TTS events status unavailable", user_id: session.user_id)
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

    case with({:ok, hash} <- hashed, do: Nats.resolve(hash)) do
      {:ok, user_id, generation} ->
        Throttle.refund(reservation)
        Logger.info("TTS session opened", user_id: user_id)
        req |> Session.put(user_id, generation) |> reply(204)

      {:not_found, reason} ->
        Logger.warning("TTS session rejected", reason: reason)
        reply(req, 404)

      {:unavailable, reason} ->
        Throttle.refund(reservation)
        Logger.warning("TTS session resolution unavailable", reason: reason)
        reply(req, 503)
    end
  end

  defp reply(req, status, headers \\ @headers),
    do: {:ok, :cowboy_req.reply(status, headers, req), nil}
end
