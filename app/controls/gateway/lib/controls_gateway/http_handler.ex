# Copyright (c) 2026 Adam Ousmer. All rights reserved.
# Proprietary. No license granted. See LICENSE.md.

defmodule ControlsGateway.HttpHandler do
  @moduledoc "GET /healthz, /readyz, /metrics."
  @behaviour :cowboy_handler

  alias ControlsGateway.{Drain, Metrics}

  @impl true
  def init(req, route), do: {:ok, respond(route, req), route}

  defp respond(:healthz, req), do: :cowboy_req.reply(200, %{}, "ok\n", req)

  defp respond(:readyz, req) do
    if Drain.ready?() do
      :cowboy_req.reply(200, %{}, "ok\n", req)
    else
      :cowboy_req.reply(503, %{}, "not ready\n", req)
    end
  end

  defp respond(:metrics, req) do
    body = Jason.encode!(Metrics.snapshot())
    :cowboy_req.reply(200, %{"content-type" => "application/json"}, body, req)
  end
end
