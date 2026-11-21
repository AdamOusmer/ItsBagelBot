# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Tts.Http do
  @behaviour :cowboy_loop
  require Logger

  alias ControlsGateway.Drain
  alias ControlsGateway.Tts.{Clips, Events, Nats, Page, Session, Throttle}

  @headers %{"cache-control" => "no-store", "referrer-policy" => "no-referrer"}
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

  defp dispatch("GET", :events, req) do
    case Drain.track_connection() do
      :ok -> serve(req, &Events.open/3)
      :draining -> reply(req, 503, Map.put(@headers, "retry-after", @drain_retry_after))
    end
  end

  defp dispatch("GET", :clip, req), do: serve(req, &Clips.reply/3)

  defp dispatch(_method, route, req) do
    allow = if route == :session, do: "POST", else: "GET"
    reply(req, 405, Map.put(@headers, "allow", allow))
  end

  defp serve(req, handler) do
    case Session.read(req) do
      {:ok, session} -> handler.(req, session, @headers)
      :error -> reply(req, 401)
    end
  end

  defp exchange(req) do
    case Throttle.reserve(client_ip(req)) do
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
    with {:ok, %{"token" => token}} when is_binary(token) and byte_size(token) == 43 <-
           Jason.decode(body),
         {:ok, raw} when byte_size(raw) == 32 <- Base.url_decode64(token, padding: false),
         ^token <- Base.url_encode64(raw, padding: false) do
      hash = :crypto.hash(:sha256, token) |> Base.encode16(case: :lower)
      Nats.resolve(hash)
    else
      _ -> :not_found
    end
  end

  defp client_ip(req) do
    {peer, _port} = :cowboy_req.peer(req)

    if Application.get_env(:controls_gateway, :overlay_trust_cloudflare_ip, false) do
      address = :cowboy_req.header("cf-connecting-ip", req, "")

      case :inet.parse_strict_address(:binary.bin_to_list(address)) do
        {:ok, ip} -> ip
        {:error, :einval} -> peer
      end
    else
      peer
    end
  end

  defp reply(req, status, headers \\ @headers),
    do: {:ok, :cowboy_req.reply(status, headers, req), nil}
end
