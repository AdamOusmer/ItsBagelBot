# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Http do
  @behaviour :cowboy_loop
  require Logger

  alias ControlsGateway.{Drain, OverlayRequest}
  alias ControlsGateway.Tts.{Clips, Events, Nats, Page, Session, Throttle}

  @headers %{"cache-control" => "no-store", "referrer-policy" => "no-referrer"}
  @allow %{session: "POST", events: "GET, HEAD"}
  @max_body 256
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

  defp serve(:events_status, req, session) do
    case Nats.check(session) do
      true -> reply(req, 200)
      false -> reply(req, 204)
      :unavailable -> reply(req, 503)
    end
  end

  defp exchange(req) do
    case Throttle.reserve(OverlayRequest.client_ip(req)) do
      {:ok, reservation} -> exchange(req, reservation)
      :limited -> reply(req, 429, Map.put(@headers, "retry-after", "60"))
    end
  end

  defp exchange(req, reservation) do
    {result, req} = resolve_token(req)

    case result do
      {:ok, user_id, generation} ->
        Throttle.refund(reservation)
        req |> Session.put(user_id, generation) |> reply(204)

      :not_found ->
        Logger.debug("TTS session rejected")
        reply(req, 404)

      :unavailable ->
        Throttle.refund(reservation)
        Logger.warning("TTS session resolution unavailable")
        reply(req, 503)
    end
  end

  defp resolve_token(req) do
    with key when is_binary(key) and byte_size(key) >= 32 <- Session.key(),
         {"application", "json", _params} <- :cowboy_req.parse_header("content-type", req) do
      read_token(req)
    else
      key when is_nil(key) or is_binary(key) -> {:unavailable, req}
      _ -> {:not_found, req}
    end
  catch
    :exit, {:request_error, _, _} -> {:not_found, req}
  end

  defp read_token(req) do
    case :cowboy_req.read_body(req, %{length: @max_body, period: 2_000}) do
      {:ok, body, req} when byte_size(body) <= @max_body -> {resolve_body(body), req}
      {_, _, req} -> {:not_found, req}
    end
  end

  defp resolve_body(body) do
    case OverlayRequest.token(body) do
      {:ok, token} ->
        :crypto.hash(:sha256, token) |> Base.encode16(case: :lower) |> Nats.resolve()

      :error ->
        :not_found
    end
  end

  defp reply(req, status, headers \\ @headers),
    do: {:ok, :cowboy_req.reply(status, headers, req), nil}
end
