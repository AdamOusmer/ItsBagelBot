# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.Powered.Http do
  @behaviour :cowboy_handler
  require Logger

  alias ControlsGateway.OverlayRequest
  alias ControlsGateway.Powered.{Assets, Nats, Page}
  alias ControlsGateway.Tts.Throttle

  @headers %{
    "cache-control" => "no-store",
    "referrer-policy" => "no-referrer",
    "x-content-type-options" => "nosniff"
  }
  @max_body 256

  @impl true
  def init(req, route), do: {:ok, dispatch(:cowboy_req.method(req), route, req), route}

  defp dispatch("GET", :page, req), do: Page.reply(req)
  defp dispatch("GET", :asset, req), do: Assets.reply(req, :cowboy_req.binding(:name, req))
  defp dispatch("POST", :config, req), do: config(req)

  defp dispatch(_method, route, req),
    do: reply(req, 405, Map.put(@headers, "allow", allowed(route)))

  defp allowed(:config), do: "POST"
  defp allowed(_route), do: "GET"

  defp config(req) do
    case Throttle.reserve(OverlayRequest.client_ip(req)) do
      {:ok, reservation} -> config(req, reservation)
      :limited -> reply(req, 429, Map.put(@headers, "retry-after", "60"))
    end
  end

  defp config(req, reservation) do
    {result, req} = read_token(req)
    respond(result, req, reservation)
  end

  defp respond({:ok, overlay}, req, reservation) do
    Throttle.refund(reservation)
    headers = Map.put(@headers, "content-type", "application/json")
    :cowboy_req.reply(200, headers, Jason.encode!(overlay), req)
  end

  defp respond(:not_found, req, _reservation) do
    Logger.debug("Powered overlay config rejected")
    reply(req, 404)
  end

  defp respond(:unavailable, req, reservation) do
    Throttle.refund(reservation)
    Logger.warning("Powered overlay config unavailable")
    reply(req, 503)
  end

  defp read_token(req) do
    case :cowboy_req.parse_header("content-type", req) do
      {"application", "json", _params} -> read_body(req)
      _ -> {:not_found, req}
    end
  catch
    :exit, {:request_error, _, _} -> {:not_found, req}
  end

  defp read_body(req) do
    case :cowboy_req.read_body(req, %{length: @max_body, period: 2_000}) do
      {:ok, body, req} when byte_size(body) <= @max_body -> {resolve(body), req}
      {_, _, req} -> {:not_found, req}
    end
  end

  defp resolve(body) do
    case OverlayRequest.token(body) do
      {:ok, token} -> Nats.resolve(token)
      :error -> :not_found
    end
  end

  defp reply(req, status, headers \\ @headers), do: :cowboy_req.reply(status, headers, req)
end
